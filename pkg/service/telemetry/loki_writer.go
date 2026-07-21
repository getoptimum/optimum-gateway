package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-common/pkg/slices"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

const (
	lokiPushPath         = "/loki/api/v1/push"
	lokiFlushInterval    = 5 * time.Second
	lokiChannelSize      = 1000
	lokiShutdownTimeout  = 10 * time.Second
	lokiChunkSize        = 1000
	lokiMaxBufferedLines = 5000
	lokiMaxErrorBodySize = 4096
	lokiJobName          = "optimum-gateway"
	// Loki stream label keys.
	lokiLabelGatewayID  = "gateway_id"
	lokiLabelOperatorID = "operator_id"
	lokiLabelCluster    = "cluster"
	lokiLabelJob        = "job"
)

type LokiWriter struct {
	lines        chan lokiLine
	data         []lokiLine        // accumulation buffer; only accessed from the run goroutine
	url          string            // pre-computed push endpoint
	headers      map[string]string // base request headers; Authorization is added per-push
	streamLabels map[string]string // fixed Loki stream labels, computed once at construction
	log          logger.AppLogger
	client       *http.Client // persistent connection pool for Loki pushes
}

type lokiLine struct {
	ts   int64 // Unix nanoseconds
	line string
}

// NewLokiWriter creates a LokiWriter. Call Start to begin flushing.
func NewLokiWriter(cfg *config.AppConfig, operatorID string, log logger.AppLogger) *LokiWriter {
	return &LokiWriter{
		lines:   make(chan lokiLine, lokiChannelSize),
		url:     strings.TrimSuffix(cfg.RemotePushLokiURL, "/") + lokiPushPath,
		headers: map[string]string{"Content-Type": "application/json"},
		streamLabels: map[string]string{
			lokiLabelGatewayID:  cfg.GatewayID,
			lokiLabelOperatorID: operatorID,
			lokiLabelCluster:    cfg.GatewayClusterID,
			lokiLabelJob:        lokiJobName,
		},
		log:    log,
		client: newMetricsHTTPClient(),
	}
}

// Write implements io.Writer. It enqueues the log line non-blocking.
func (w *LokiWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if line == "" {
		return len(p), nil
	}
	entry := lokiLine{ts: time.Now().UnixNano(), line: line}
	select {
	case w.lines <- entry:
	default:
		// buffer full — drop line rather than block the logger
	}
	return len(p), nil
}

// Start launches the background flush goroutine. The returned channel is
// closed once the final flush has completed so callers can wait for clean shutdown.
func (w *LokiWriter) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go w.run(ctx, done) //nolint:gosec // G118: ctx drives lifecycle; Background used only for post-cancel flush
	return done
}

func (w *LokiWriter) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(lokiFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			finalCtx, finalCancel := context.WithTimeout(context.Background(), lokiShutdownTimeout)
			if err := w.flush(finalCtx); err != nil {
				w.log.Error("loki: final flush", err)
			}
			finalCancel()
			return
		case l := <-w.lines:
			w.data = slices.AppendBounded(w.data, l, lokiMaxBufferedLines)
		case <-ticker.C:
			if err := w.flush(ctx); err != nil {
				w.log.Error("loki: flush", err)
			}
		}
	}
}

func (w *LokiWriter) flush(ctx context.Context) error {
	if len(w.data) == 0 {
		return nil // nothing buffered
	}
	chunks := slices.ChunkSlice(w.data, lokiChunkSize)
	var nonRetryableErr error
	for i := range chunks {
		if len(chunks[i]) == 0 {
			continue
		}
		if err := w.flushChunk(ctx, chunks[i]); err != nil {
			if isRetryableLokiError(err) {
				w.retainTail(chunks[i:])
				return fmt.Errorf("flush chunk %d/%d: %w", i+1, len(chunks), err)
			}
			nonRetryableErr = fmt.Errorf("flush chunk %d/%d: %w", i+1, len(chunks), err)
			continue
		}
	}
	w.releaseBuffer()
	return nonRetryableErr
}

func (w *LokiWriter) flushChunk(ctx context.Context, data []lokiLine) error {
	ctx, cancel := context.WithTimeout(ctx, scrapePushTimeout)
	defer cancel()

	headers, err := pushAuthHeaders(w.headers)
	if err != nil {
		return err
	}

	values := make([][]string, 0, len(data))
	for _, e := range data {
		values = append(values, []string{strconv.FormatInt(e.ts, 10), e.line})
	}
	body := lokiPushBody{
		Streams: []lokiStream{
			{
				Stream: w.streamLabels,
				Values: values,
			},
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	var responseBody string
	_, statusCode, err := commonnet.CurlWithBody[any](ctx, http.MethodPost, w.url, bodyBytes, headers,
		commonnet.WithHTTPClient[any](w.client),
		commonnet.WithDecoder[any](func(r io.Reader) error {
			body, readErr := io.ReadAll(io.LimitReader(r, lokiMaxErrorBodySize))
			if readErr != nil {
				return readErr
			}
			responseBody = strings.TrimSpace(string(body))
			return nil
		}))
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if statusCode/100 != 2 {
		return lokiStatusError{statusCode: statusCode, body: responseBody}
	}
	return nil
}

func (w *LokiWriter) retainTail(chunks [][]lokiLine) {
	w.data = slices.ConcatKeepLast(chunks, lokiMaxBufferedLines)
}

func (w *LokiWriter) releaseBuffer() {
	nextCap := min(cap(w.data), lokiMaxBufferedLines)
	w.data = make([]lokiLine, 0, nextCap)
}

func isRetryableLokiError(err error) bool {
	var statusErr lokiStatusError
	if !errors.As(err, &statusErr) {
		return true
	}
	return statusErr.statusCode == http.StatusTooManyRequests || statusErr.statusCode/100 == 5
}

type lokiStatusError struct {
	statusCode int
	body       string
}

func (e lokiStatusError) Error() string {
	if e.body != "" {
		return fmt.Sprintf("unexpected status: %d: %s", e.statusCode, e.body)
	}
	return fmt.Sprintf("unexpected status: %d", e.statusCode)
}

type lokiPushBody struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}
