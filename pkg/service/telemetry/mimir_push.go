package telemetry

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/golang/snappy"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/proto"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/remotepb"
)

const (
	mimirPushInterval    = 15 * time.Second
	mimirPushPath        = "/api/v1/push"
	mimirShutdownTimeout = 15 * time.Second
)

func startMimirPush(ctx context.Context, log logger.AppLogger, cfg *config.AppConfig) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(mimirPushInterval)
		defer ticker.Stop()

		client := newMetricsHTTPClient()
		url := strings.TrimSuffix(cfg.RemotePushMimirURL, "/") + mimirPushPath

		log.Info("starting mimir remote write push", logger.WithString("url", url))

		delay := backoffBaseDelay
		for {
			select {
			case <-ctx.Done():
				mimirFinalPush(client, url, log)
				return
			case <-ticker.C:
				pushCtx, pushCancel := context.WithTimeout(ctx, scrapePushTimeout)
				err := pushMetricsToMimir(pushCtx, log, client, url)
				pushCancel()
				if err != nil {
					log.Error("could not push metrics to mimir", err)
					delay *= 2
					if delay > backoffMaxDelay {
						delay = backoffMaxDelay
					}
					timer := time.NewTimer(delay)
					select {
					case <-ctx.Done():
						timer.Stop()
						mimirFinalPush(client, url, log)
						return
					case <-timer.C:
					}
				} else {
					delay = backoffBaseDelay
				}
			}
		}
	}()
	return done
}

// mimirFinalPush performs a last-chance metrics push using a fresh context so
// the HTTP request is not immediately canceled by the already-done parent context.
// A dedicated client with a matching Timeout is used so the hard client-level
// 5 s deadline from newMetricsHTTPClient does not override mimirShutdownTimeout.
func mimirFinalPush(client *http.Client, url string, log logger.AppLogger) {
	ctx, cancel := context.WithTimeout(context.Background(), mimirShutdownTimeout)
	defer cancel()
	shutdownClient := &http.Client{
		Transport: client.Transport,
		Timeout:   mimirShutdownTimeout,
	}
	if err := pushMetricsToMimir(ctx, log, shutdownClient, url); err != nil {
		log.Error("mimir: final push failed", err)
	}
}

var mimirPushHeaders = map[string]string{
	"Content-Type":                      "application/x-protobuf",
	"Content-Encoding":                  "snappy",
	"X-Prometheus-Remote-Write-Version": "0.1.0",
}

func pushMetricsToMimir(ctx context.Context, log logger.AppLogger, client *http.Client, url string) error {
	headers, err := pushAuthHeaders(mimirPushHeaders)
	if err != nil {
		return err
	}

	families, err := CustomRegistry.Gather()
	if err != nil {
		return fmt.Errorf("gather metrics: %w", err)
	}

	wr := &remotepb.WriteRequest{
		Timeseries: metricFamiliesToTimeSeries(log, families),
	}
	if len(wr.Timeseries) == 0 {
		return nil
	}

	raw, err := proto.Marshal(wr)
	if err != nil {
		return fmt.Errorf("marshal write request: %w", err)
	}
	compressed := snappy.Encode(nil, raw)

	_, statusCode, err := commonnet.CurlWithBody[any](ctx, http.MethodPost, url, compressed, headers,
		commonnet.WithHTTPClient[any](client))
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	if statusCode/100 != 2 {
		return fmt.Errorf("unexpected status: %d", statusCode)
	}
	return nil
}

// metricFamiliesToTimeSeries converts gathered dto.MetricFamily slices into
// remotepb.TimeSeries entries for Prometheus remote write.
func metricFamiliesToTimeSeries(log logger.AppLogger, families []*dto.MetricFamily) []*remotepb.TimeSeries {
	now := time.Now().UnixMilli()
	estimate := 0
	for _, mf := range families {
		estimate += len(mf.GetMetric())
	}
	out := make([]*remotepb.TimeSeries, 0, estimate)

	for _, mf := range families {
		name := mf.GetName()
		for _, m := range mf.GetMetric() {
			baseLabels := dtoLabelsToRemote(m.GetLabel())

			switch mf.GetType() {
			case dto.MetricType_COUNTER:
				out = append(out, &remotepb.TimeSeries{
					Labels:  withName(baseLabels, name),
					Samples: []*remotepb.Sample{{Value: m.GetCounter().GetValue(), Timestamp: now}},
				})

			case dto.MetricType_GAUGE:
				out = append(out, &remotepb.TimeSeries{
					Labels:  withName(baseLabels, name),
					Samples: []*remotepb.Sample{{Value: m.GetGauge().GetValue(), Timestamp: now}},
				})

			case dto.MetricType_SUMMARY:
				s := m.GetSummary()
				out = append(out,
					&remotepb.TimeSeries{
						Labels:  withName(baseLabels, name+"_sum"),
						Samples: []*remotepb.Sample{{Value: s.GetSampleSum(), Timestamp: now}},
					},
					&remotepb.TimeSeries{
						Labels:  withName(baseLabels, name+"_count"),
						Samples: []*remotepb.Sample{{Value: float64(s.GetSampleCount()), Timestamp: now}},
					},
				)
				for _, q := range s.GetQuantile() {
					out = append(out, &remotepb.TimeSeries{
						Labels:  withName(appendLabel(baseLabels, "quantile", strconv.FormatFloat(q.GetQuantile(), 'g', -1, 64)), name),
						Samples: []*remotepb.Sample{{Value: q.GetValue(), Timestamp: now}},
					})
				}

			case dto.MetricType_HISTOGRAM:
				out = appendHistogramBuckets(out, m.GetHistogram(), baseLabels, name, now)

			case dto.MetricType_GAUGE_HISTOGRAM:
				// GAUGE_HISTOGRAM reuses the histogram field per the client_model proto spec.
				out = appendHistogramBuckets(out, m.GetHistogram(), baseLabels, name, now)

			case dto.MetricType_UNTYPED:
				out = append(out, &remotepb.TimeSeries{
					Labels:  withName(baseLabels, name),
					Samples: []*remotepb.Sample{{Value: m.GetUntyped().GetValue(), Timestamp: now}},
				})

			default:
				log.Error("mimir: unhandled metric type — skipping",
					fmt.Errorf("type=%v name=%q", mf.GetType(), name))
				continue
			}
		}
	}
	return out
}

func dtoLabelsToRemote(pairs []*dto.LabelPair) []*remotepb.Label {
	out := make([]*remotepb.Label, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, &remotepb.Label{Name: p.GetName(), Value: p.GetValue()})
	}
	return out
}

// withName prepends the __name__ label (Prometheus convention) to a label set
// and returns the slice sorted lexicographically by label name, as required by
// the Prometheus remote write specification.
func withName(labels []*remotepb.Label, name string) []*remotepb.Label {
	out := append([]*remotepb.Label{{Name: "__name__", Value: name}}, labels...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// appendLabel returns a new slice with an extra label appended (does not modify the original).
func appendLabel(labels []*remotepb.Label, name, value string) []*remotepb.Label {
	out := make([]*remotepb.Label, len(labels)+1)
	copy(out, labels)
	out[len(labels)] = &remotepb.Label{Name: name, Value: value}
	return out
}

// appendHistogramBuckets appends _sum, _count, and per-bucket TimeSeries for a
// histogram-like metric (HISTOGRAM or GAUGE_HISTOGRAM). Buckets with +Inf upper
// bound inside GetBucket() are skipped; the explicit +Inf entry is always
// appended at the end using SampleCount to avoid duplicates.
func appendHistogramBuckets(out []*remotepb.TimeSeries, h *dto.Histogram, baseLabels []*remotepb.Label, name string, now int64) []*remotepb.TimeSeries {
	out = append(out,
		&remotepb.TimeSeries{
			Labels:  withName(baseLabels, name+"_sum"),
			Samples: []*remotepb.Sample{{Value: h.GetSampleSum(), Timestamp: now}},
		},
		&remotepb.TimeSeries{
			Labels:  withName(baseLabels, name+"_count"),
			Samples: []*remotepb.Sample{{Value: float64(h.GetSampleCount()), Timestamp: now}},
		},
	)
	for _, b := range h.GetBucket() {
		if math.IsInf(b.GetUpperBound(), 1) {
			continue // skip +Inf; appended explicitly below
		}
		le := strconv.FormatFloat(b.GetUpperBound(), 'g', -1, 64)
		out = append(out, &remotepb.TimeSeries{
			Labels:  withName(appendLabel(baseLabels, "le", le), name+"_bucket"),
			Samples: []*remotepb.Sample{{Value: float64(b.GetCumulativeCount()), Timestamp: now}},
		})
	}
	out = append(out, &remotepb.TimeSeries{
		Labels:  withName(appendLabel(baseLabels, "le", "+Inf"), name+"_bucket"),
		Samples: []*remotepb.Sample{{Value: float64(h.GetSampleCount()), Timestamp: now}},
	})
	return out
}
