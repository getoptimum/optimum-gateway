package telemetry

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/remotepb"
)

// helpers

func labelNames(ts *remotepb.TimeSeries) []string {
	names := make([]string, 0, len(ts.Labels))
	for _, l := range ts.Labels {
		names = append(names, l.Name)
	}
	return names
}

func findSeries(series []*remotepb.TimeSeries, name string) *remotepb.TimeSeries {
	for _, ts := range series {
		for _, l := range ts.Labels {
			if l.Name == "__name__" && l.Value == name {
				return ts
			}
		}
	}
	return nil
}

func findSeriesWithLabel(series []*remotepb.TimeSeries, name, labelName, labelValue string) *remotepb.TimeSeries {
	for _, ts := range series {
		hasName := false
		hasLabel := false
		for _, l := range ts.Labels {
			if l.Name == "__name__" && l.Value == name {
				hasName = true
			}
			if l.Name == labelName && l.Value == labelValue {
				hasLabel = true
			}
		}
		if hasName && hasLabel {
			return ts
		}
	}
	return nil
}

func isSorted(ts *remotepb.TimeSeries) bool {
	return sort.SliceIsSorted(ts.Labels, func(i, j int) bool {
		return ts.Labels[i].Name < ts.Labels[j].Name
	})
}

// --- metricFamiliesToTimeSeries ---

func TestMetricFamiliesToTimeSeries_Counter(t *testing.T) {
	families := []*dto.MetricFamily{
		{
			Name: new("http_requests_total"),
			Type: new(dto.MetricType_COUNTER),
			Metric: []*dto.Metric{
				{Counter: &dto.Counter{Value: new(float64(42))}},
			},
		},
	}
	series := metricFamiliesToTimeSeries(logger.NewAppSLogger(logger.Debug), families)
	require.Len(t, series, 1)
	ts := series[0]
	require.Equal(t, 42.0, ts.Samples[0].Value)
	require.True(t, isSorted(ts), "labels not sorted: %v", labelNames(ts))
	require.NotNil(t, findSeries(series, "http_requests_total"), "__name__ label not present or wrong value")
}

func TestMetricFamiliesToTimeSeries_ScalarTypes(t *testing.T) {
	cases := []struct {
		name    string
		family  *dto.MetricFamily
		wantVal float64
	}{
		{
			name: "gauge",
			family: &dto.MetricFamily{
				Name: new("memory_bytes"),
				Type: new(dto.MetricType_GAUGE),
				Metric: []*dto.Metric{
					{Gauge: &dto.Gauge{Value: new(float64(1024))}},
				},
			},
			wantVal: 1024,
		},
		{
			name: "untyped",
			family: &dto.MetricFamily{
				Name: new("some_untyped"),
				Type: new(dto.MetricType_UNTYPED),
				Metric: []*dto.Metric{
					{Untyped: &dto.Untyped{Value: new(float64(7))}},
				},
			},
			wantVal: 7,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			series := metricFamiliesToTimeSeries(logger.NewAppSLogger(logger.Debug), []*dto.MetricFamily{tc.family})
			require.Len(t, series, 1)
			require.Equal(t, tc.wantVal, series[0].Samples[0].Value)
		})
	}
}

func TestMetricFamiliesToTimeSeries_Summary(t *testing.T) {
	families := []*dto.MetricFamily{
		{
			Name: new("latency"),
			Type: new(dto.MetricType_SUMMARY),
			Metric: []*dto.Metric{
				{
					Summary: &dto.Summary{
						SampleSum:   new(100.5),
						SampleCount: new(uint64(10)),
						Quantile: []*dto.Quantile{
							{Quantile: new(0.5), Value: new(9.5)},
							{Quantile: new(0.99), Value: new(19.9)},
						},
					},
				},
			},
		},
	}
	series := metricFamiliesToTimeSeries(logger.NewAppSLogger(logger.Debug), families)
	// _sum, _count, 2 quantiles
	require.Len(t, series, 4)
	sum := findSeries(series, "latency_sum")
	require.NotNil(t, sum, "_sum series missing")
	require.Equal(t, 100.5, sum.Samples[0].Value, "_sum wrong value")
	count := findSeries(series, "latency_count")
	require.NotNil(t, count, "_count series missing")
	require.Equal(t, 10.0, count.Samples[0].Value, "_count wrong value")
	q50 := findSeriesWithLabel(series, "latency", "quantile", "0.5")
	require.NotNil(t, q50, "quantile=0.5 series missing")
	require.True(t, isSorted(q50), "quantile series labels not sorted: %v", labelNames(q50))
	require.NotNil(t, findSeriesWithLabel(series, "latency", "quantile", "0.99"), "quantile=0.99 series missing")
}

func TestMetricFamiliesToTimeSeries_Histogram_NoDuplicateInf(t *testing.T) {
	// Include a +Inf bucket explicitly inside GetBucket() — must not create a duplicate.
	families := []*dto.MetricFamily{
		{
			Name: new("req_duration"),
			Type: new(dto.MetricType_HISTOGRAM),
			Metric: []*dto.Metric{
				{
					Histogram: &dto.Histogram{
						SampleSum:   new(float64(50)),
						SampleCount: new(uint64(5)),
						Bucket: []*dto.Bucket{
							{UpperBound: new(0.1), CumulativeCount: new(uint64(2))},
							{UpperBound: new(1.0), CumulativeCount: new(uint64(4))},
							// +Inf explicitly in GetBucket — should be skipped
							{UpperBound: new(math.Inf(1)), CumulativeCount: new(uint64(5))}, // +Inf
						},
					},
				},
			},
		},
	}
	series := metricFamiliesToTimeSeries(logger.NewAppSLogger(logger.Debug), families)

	// Count how many series have __name__=req_duration_bucket and le=+Inf
	infCount := 0
	for _, ts := range series {
		hasNameBucket := false
		hasInfLe := false
		for _, l := range ts.Labels {
			if l.Name == "__name__" && l.Value == "req_duration_bucket" {
				hasNameBucket = true
			}
			if l.Name == "le" && l.Value == "+Inf" {
				hasInfLe = true
			}
		}
		if hasNameBucket && hasInfLe {
			infCount++
		}
	}
	require.Equal(t, 1, infCount, "expected exactly 1 +Inf bucket series")
}

func TestMetricFamiliesToTimeSeries_GaugeHistogram(t *testing.T) {
	// GAUGE_HISTOGRAM reuses m.GetHistogram() — same shape as HISTOGRAM.
	families := []*dto.MetricFamily{
		{
			Name: new("size"),
			Type: new(dto.MetricType_GAUGE_HISTOGRAM),
			Metric: []*dto.Metric{
				{
					Histogram: &dto.Histogram{
						SampleSum:   new(float64(200)),
						SampleCount: new(uint64(8)),
						Bucket: []*dto.Bucket{
							{UpperBound: new(float64(64)), CumulativeCount: new(uint64(3))},
						},
					},
				},
			},
		},
	}
	series := metricFamiliesToTimeSeries(logger.NewAppSLogger(logger.Debug), families)
	// _sum, _count, finite bucket, explicit +Inf = 4 series
	require.Len(t, series, 4, "expected 4 series for GAUGE_HISTOGRAM")
	require.NotNil(t, findSeries(series, "size_sum"), "size_sum series missing")
	require.NotNil(t, findSeries(series, "size_count"), "size_count series missing")
}

// --- appendHistogramBuckets ---

func TestAppendHistogramBuckets_SkipsInfInBucketList(t *testing.T) {
	h := &dto.Histogram{
		SampleSum:   new(float64(10)),
		SampleCount: new(uint64(3)),
		Bucket: []*dto.Bucket{
			{UpperBound: new(1.0), CumulativeCount: new(uint64(2))},
			// +Inf inside the bucket list — must be skipped
			{UpperBound: new(math.Inf(1)), CumulativeCount: new(uint64(3))},
		},
	}
	out := appendHistogramBuckets(nil, h, nil, "hist", 0)

	infCount := 0
	for _, ts := range out {
		for _, l := range ts.Labels {
			if l.Name == "le" && l.Value == "+Inf" {
				infCount++
			}
		}
	}
	require.Equal(t, 1, infCount, "expected exactly 1 +Inf le label")
}

func TestAppendHistogramBuckets_Structure(t *testing.T) {
	h := &dto.Histogram{
		SampleSum:   new(float64(30)),
		SampleCount: new(uint64(6)),
		Bucket: []*dto.Bucket{
			{UpperBound: new(0.5), CumulativeCount: new(uint64(1))},
			{UpperBound: new(2.0), CumulativeCount: new(uint64(4))},
		},
	}
	out := appendHistogramBuckets(nil, h, nil, "hist", 0)
	// _sum, _count, 2 finite buckets, +Inf = 5
	require.Len(t, out, 5)
	for _, ts := range out {
		require.True(t, isSorted(ts), "labels not sorted: %v", labelNames(ts))
	}
}

// --- Mimir shutdown integration test ---

// TestInitMetrics_MimirDone verifies InitMetrics wiring: non-nil done channel when
// remote push is enabled, sync.Once returns the same channel, and cancel closes it.
func TestInitMetrics_MimirDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	prevRegistry := CustomRegistry
	prevMimirDone := mimirDone
	t.Cleanup(func() {
		CustomRegistry = prevRegistry
		mimirDone = prevMimirDone
		oncer = sync.Once{}
	})
	CustomRegistry = nil
	mimirDone = nil
	oncer = sync.Once{}

	cfg := &config.AppConfig{
		TelemetryEnable:    true,
		RemotePushEnable:   true,
		RemotePushMimirURL: srv.URL,
		GatewayID:          testGatewayID,
		GatewayClusterID:   testGatewayClusterID,
	}
	setTestToken(t)
	log := logger.NewAppSLogger(logger.Debug)

	ctx, cancel := context.WithCancel(context.Background())
	ch := InitMetrics(ctx, log, cfg)
	require.NotNil(t, ch, "InitMetrics returned nil channel when RemotePushEnable=true")

	ch2 := InitMetrics(ctx, log, cfg)
	require.Equal(t, ch, ch2, "second InitMetrics call should return the same channel")

	cancel()
	select {
	case <-ch:
	case <-time.After(mimirShutdownTimeout + 2*time.Second):
		t.Fatal("mimirDone channel did not close within expected timeout")
	}
}

// TestMimirShutdown verifies that canceling the context causes startMimirPush to
// close its done channel and attempt a final push to the remote endpoint.
func TestMimirShutdown(t *testing.T) {
	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == mimirPushPath {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			pushCount.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	// Set up a non-nil CustomRegistry with at least one metric so pushMetricsToMimir
	// doesn't return early (it skips the HTTP call when there are zero time series).
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_shutdown_gauge"})
	reg.MustRegister(g)
	g.Set(1)
	// Reset the package-level registry after the test.
	CustomRegistry = reg
	t.Cleanup(func() { CustomRegistry = prometheus.NewRegistry() })

	cfg := &config.AppConfig{
		RemotePushMimirURL: srv.URL,
	}
	log := logger.NewAppSLogger(logger.Debug)

	setTestToken(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := startMimirPush(ctx, log, cfg)

	// Cancel immediately to trigger the final-push path.
	cancel()

	select {
	case <-done:
		// good — goroutine finished
	case <-time.After(mimirShutdownTimeout + 2*time.Second):
		t.Fatal("done channel did not close within expected timeout")
	}

	require.Positive(t, pushCount.Load(), "expected at least one push request during shutdown, got none")
}
