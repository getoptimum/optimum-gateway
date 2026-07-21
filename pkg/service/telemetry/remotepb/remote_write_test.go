package remotepb

import (
	"bytes"
	"encoding/hex"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
)

var update = flag.Bool("update", false, "update golden files")

const promNameLabel = "__name__"

func goldenPath(name string) string {
	return filepath.Join("testdata", name+".golden")
}

func marshal(t *testing.T, wr *WriteRequest) []byte {
	t.Helper()
	b, err := proto.Marshal(wr)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	return b
}

func runGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := goldenPath(name)
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("marshal output mismatch for %s\ngot  %s\nwant %s", name,
			hex.EncodeToString(got), hex.EncodeToString(want))
	}
}

// TestMarshalCounter exercises a single counter time series.
func TestMarshalCounter(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "http_requests_total"},
					{Name: "gateway_id", Value: "gw1"},
				},
				Samples: []*Sample{
					{Value: 42.0, Timestamp: 1700000000000},
				},
			},
		},
	}
	runGolden(t, "counter", marshal(t, wr))
}

// TestMarshalGauge exercises a single gauge time series.
func TestMarshalGauge(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "memory_bytes"},
				},
				Samples: []*Sample{
					{Value: 1048576.0, Timestamp: 1700000001000},
				},
			},
		},
	}
	runGolden(t, "gauge", marshal(t, wr))
}

// TestMarshalHistogram exercises histogram-style series (sum, count, multiple buckets).
// Uses distinct metric names and three explicit le buckets to differ structurally from TestMarshalSummary.
func TestMarshalHistogram(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels:  []*Label{{Name: promNameLabel, Value: "process_cpu_seconds_sum"}, {Name: "job", Value: "gw"}},
				Samples: []*Sample{{Value: 9.87, Timestamp: 1700000002000}},
			},
			{
				Labels:  []*Label{{Name: promNameLabel, Value: "process_cpu_seconds_count"}, {Name: "job", Value: "gw"}},
				Samples: []*Sample{{Value: 1000.0, Timestamp: 1700000002000}},
			},
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "process_cpu_seconds_bucket"},
					{Name: "job", Value: "gw"},
					{Name: "le", Value: "0.5"},
				},
				Samples: []*Sample{{Value: 800.0, Timestamp: 1700000002000}},
			},
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "process_cpu_seconds_bucket"},
					{Name: "job", Value: "gw"},
					{Name: "le", Value: "1.0"},
				},
				Samples: []*Sample{{Value: 950.0, Timestamp: 1700000002000}},
			},
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "process_cpu_seconds_bucket"},
					{Name: "job", Value: "gw"},
					{Name: "le", Value: "+Inf"},
				},
				Samples: []*Sample{{Value: 1000.0, Timestamp: 1700000002000}},
			},
		},
	}
	runGolden(t, "histogram", marshal(t, wr))
}

// TestMarshalSummary exercises summary-style series (sum, count, quantiles).
func TestMarshalSummary(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels:  []*Label{{Name: promNameLabel, Value: "latency_sum"}},
				Samples: []*Sample{{Value: 100.5, Timestamp: 1700000003000}},
			},
			{
				Labels:  []*Label{{Name: promNameLabel, Value: "latency_count"}},
				Samples: []*Sample{{Value: 10.0, Timestamp: 1700000003000}},
			},
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "latency"},
					{Name: "quantile", Value: "0.5"},
				},
				Samples: []*Sample{{Value: 9.5, Timestamp: 1700000003000}},
			},
			{
				Labels: []*Label{
					{Name: promNameLabel, Value: "latency"},
					{Name: "quantile", Value: "0.99"},
				},
				Samples: []*Sample{{Value: 19.9, Timestamp: 1700000003000}},
			},
		},
	}
	runGolden(t, "summary", marshal(t, wr))
}

// TestMarshalEmptyLabels exercises a series with no extra labels.
func TestMarshalEmptyLabels(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels:  []*Label{{Name: promNameLabel, Value: "up"}},
				Samples: []*Sample{{Value: 1.0, Timestamp: 1700000004000}},
			},
		},
	}
	runGolden(t, "empty_labels", marshal(t, wr))
}

// TestMarshalEmptyRequest exercises an empty WriteRequest.
func TestMarshalEmptyRequest(t *testing.T) {
	wr := &WriteRequest{}
	got := marshal(t, wr)
	if len(got) != 0 {
		t.Errorf("empty WriteRequest should marshal to zero bytes, got %d: %s",
			len(got), hex.EncodeToString(got))
	}
}

// TestMarshalLargeVarint exercises a timestamp that requires multi-byte varint encoding.
func TestMarshalLargeVarint(t *testing.T) {
	wr := &WriteRequest{
		Timeseries: []*TimeSeries{
			{
				Labels:  []*Label{{Name: promNameLabel, Value: "ts_large"}},
				Samples: []*Sample{{Value: 0.0, Timestamp: 9999999999999}},
			},
		},
	}
	runGolden(t, "large_varint", marshal(t, wr))
}
