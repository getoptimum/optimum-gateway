package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

// setTestToken seeds the package-level push token so flushChunk /
// pushAuthHeaders succeed. Each test that needs a push calls this in setup.
func setTestToken(t *testing.T) {
	t.Helper()
	SetPushToken("test-token")
	t.Cleanup(func() { SetPushToken("") })
}

// TestLokiWriterShutdown verifies that canceling the context drains queued
// entries and closes the done channel returned by Start.
func TestLokiWriterShutdown(t *testing.T) {
	setTestToken(t)
	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			pushCount.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.AppConfig{
		RemotePushLokiURL: srv.URL,
		GatewayID:         testGatewayID,
		GatewayClusterID:  testGatewayClusterID,
	}
	w := NewLokiWriter(cfg, "acme-operator", logger.NewAppSLogger(logger.Debug))

	ctx, cancel := context.WithCancel(context.Background())
	done := w.Start(ctx)

	// Write entries and let the run goroutine move them from the channel to the
	// accumulation buffer before we trigger shutdown.
	for range 10 {
		_, _ = w.Write([]byte("test log line\n"))
	}
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case <-done:
		// good
	case <-time.After(lokiShutdownTimeout + 2*time.Second):
		t.Fatal("done channel did not close within expected timeout")
	}

	if pushCount.Load() == 0 {
		t.Error("expected at least one Loki push during shutdown, got none")
	}
}

// TestLokiWriter_OperatorIDLabel verifies operator_id is emitted as a stream
// label on every push, and that the deprecated partner_id label is not.
func TestLokiWriter_OperatorIDLabel(t *testing.T) {
	setTestToken(t)
	var mu sync.Mutex
	var captured []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body, _ := io.ReadAll(r.Body)
			_ = r.Body.Close()
			mu.Lock()
			captured = body
			mu.Unlock()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.AppConfig{
		RemotePushLokiURL: srv.URL,
		GatewayID:         testGatewayID,
		GatewayClusterID:  testGatewayClusterID,
	}
	w := NewLokiWriter(cfg, "acme-operator", logger.NewAppSLogger(logger.Debug))

	ctx, cancel := context.WithCancel(context.Background())
	done := w.Start(ctx)

	_, _ = w.Write([]byte("hello\n"))
	time.Sleep(50 * time.Millisecond) // let the goroutine move the line to the accumulation buffer
	cancel()

	select {
	case <-done:
	case <-time.After(lokiShutdownTimeout + 2*time.Second):
		t.Fatal("done channel did not close within expected timeout")
	}

	mu.Lock()
	body := captured
	mu.Unlock()

	if len(body) == 0 {
		t.Fatal("no push body captured")
	}

	var parsed struct {
		Streams []struct {
			Stream map[string]string `json:"stream"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("failed to parse Loki push body: %v\nbody: %s", err, body)
	}
	if len(parsed.Streams) == 0 {
		t.Fatal("expected at least one stream in Loki push body")
	}
	require.Equal(t, "acme-operator", parsed.Streams[0].Stream["operator_id"])
	require.NotContains(t, parsed.Streams[0].Stream, "partner_id", "partner_id label must not be emitted")
}

// TestLokiWriterDropsOnFullBuffer verifies that Write never blocks even when
// the internal channel is full.
func TestLokiWriterDropsOnFullBuffer(t *testing.T) {
	// Use a URL that will never respond to ensure nothing drains.
	cfg := &config.AppConfig{
		RemotePushLokiURL: "http://127.0.0.1:0",
		GatewayID:         testGatewayID,
		GatewayClusterID:  testGatewayClusterID,
	}
	w := NewLokiWriter(cfg, "acme-operator", logger.NewAppSLogger(logger.Debug))
	// Do not call Start — channel stays full once capacity is reached.
	for range lokiChannelSize + 100 {
		n, err := w.Write([]byte("line\n"))
		if err != nil {
			t.Fatalf("Write returned error: %v", err)
		}
		if n != 5 {
			t.Fatalf("Write returned n=%d, want 5", n)
		}
	}
}

func TestLokiWriterDropsBadRequestChunks(t *testing.T) {
	setTestToken(t)
	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		pushCount.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("entry timestamp too old"))
	}))
	t.Cleanup(srv.Close)

	cfg := &config.AppConfig{
		RemotePushLokiURL: srv.URL,
		GatewayID:         testGatewayID,
		GatewayClusterID:  testGatewayClusterID,
	}
	w := NewLokiWriter(cfg, "acme-operator", logger.NewAppSLogger(logger.Debug))
	w.data = makeLokiLines(2500)

	if err := w.flush(context.Background()); err == nil {
		t.Fatal("expected flush to report the non-retryable Loki error")
	} else if !strings.Contains(err.Error(), "entry timestamp too old") {
		t.Fatalf("expected Loki response body in error, got %v", err)
	}
	if got := len(w.data); got != 0 {
		t.Fatalf("bad-request chunks should be dropped, retained %d lines", got)
	}
	if got := pushCount.Load(); got != 3 {
		t.Fatalf("push count = %d, want 3 chunks attempted", got)
	}
}

func TestLokiWriterRetainsBoundedTailOnRetryableFailure(t *testing.T) {
	setTestToken(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	cfg := &config.AppConfig{
		RemotePushLokiURL: srv.URL,
		GatewayID:         testGatewayID,
		GatewayClusterID:  testGatewayClusterID,
	}
	w := NewLokiWriter(cfg, "acme-operator", logger.NewAppSLogger(logger.Debug))
	w.data = makeLokiLines(lokiMaxBufferedLines + 2000)

	if err := w.flush(context.Background()); err == nil {
		t.Fatal("expected flush to report the retryable Loki error")
	}
	if got := len(w.data); got != lokiMaxBufferedLines {
		t.Fatalf("retained lines = %d, want %d", got, lokiMaxBufferedLines)
	}
	if got := w.data[0].line; got != "line-2000" {
		t.Fatalf("first retained line = %q, want newest bounded tail starting at line-2000", got)
	}
}

func makeLokiLines(n int) []lokiLine {
	lines := make([]lokiLine, 0, n)
	for i := range n {
		lines = append(lines, lokiLine{ts: int64(i), line: fmt.Sprintf("line-%d", i)})
	}
	return lines
}
