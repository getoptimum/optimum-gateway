package telemetry

import (
	"maps"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// protocolIdentityLabels are copied onto mump2p_* series at scrape time.
// mump2p-protocol registers that set on prometheus.DefaultRegisterer. The
// gateway remote-writes CustomRegistry, and every gateway would otherwise
// publish those series under the same localhost scrape target.
var protocolIdentityLabels atomic.Pointer[[]*dto.LabelPair]

// SetProtocolIdentityLabels stamps these labels onto protocol metrics when
// /metrics is scraped. InitMetrics passes the same labels the gateway registry
// already wraps around its own series.
func SetProtocolIdentityLabels(labels prometheus.Labels) {
	if len(labels) == 0 {
		protocolIdentityLabels.Store(nil)
		return
	}
	keys := slices.Sorted(maps.Keys(labels))
	pairs := make([]*dto.LabelPair, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, &dto.LabelPair{
			Name:  new(key),
			Value: new(labels[key]),
		})
	}
	protocolIdentityLabels.Store(&pairs)
}

// protocolMetricGatherer returns mump2p_* families from the default registry.
// Go and process collectors also live there and are already registered on
// CustomRegistry, so they stay out of this gatherer.
type protocolMetricGatherer struct{}

func (protocolMetricGatherer) Gather() ([]*dto.MetricFamily, error) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return nil, err
	}
	extra := protocolIdentityLabels.Load()
	out := make([]*dto.MetricFamily, 0)
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "mump2p_") {
			continue
		}
		if extra != nil {
			stampProtocolLabels(family, *extra)
		}
		out = append(out, family)
	}
	return out, nil
}

func stampProtocolLabels(family *dto.MetricFamily, extra []*dto.LabelPair) {
	for _, metric := range family.GetMetric() {
		seen := make(map[string]struct{}, len(metric.Label))
		for _, lp := range metric.Label {
			seen[lp.GetName()] = struct{}{}
		}
		labels := slices.Clone(metric.Label)
		for _, lp := range extra {
			if _, ok := seen[lp.GetName()]; ok {
				continue
			}
			labels = append(labels, lp)
		}
		slices.SortFunc(labels, func(a, b *dto.LabelPair) int {
			return strings.Compare(a.GetName(), b.GetName())
		})
		metric.Label = labels
	}
}
