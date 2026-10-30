package mum_p2p

import (
	"log/slog"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	protocoltelemetry "github.com/getoptimum/mump2p-protocol/pkg/telemetry"
	"github.com/getoptimum/mump2p-protocol/pkg/telemetry/rlnctrace"
)

// enableProtocolMetrics registers the protocol metric set and returns the tracer
// that fills it. Partial-control queues, stage wait, recode, relay credits, and
// shared-memory lanes are recorded by that tracer and the SHM observer. The
// gateway scrape in telemetry includes the resulting mump2p_* series.
func enableProtocolMetrics(
	psCfg *mump2pcfg.Config,
	clusterID, peerID string,
	log *slog.Logger,
	shm *RLNCWrapper,
) (rlnctrace.RLNCTracer, error) {
	metrics, err := newProtocolMetrics(psCfg, clusterID, peerID, log)
	if err != nil {
		return nil, err
	}
	shm.srv.SetObserver(protocoltelemetry.NewSHMCollector(metrics))
	return protocoltelemetry.NewTraceCollector(metrics), nil
}

func newProtocolMetrics(
	psCfg *mump2pcfg.Config,
	clusterID, peerID string,
	log *slog.Logger,
) (*protocoltelemetry.Metrics, error) {
	metricCfg := *psCfg
	metricCfg.ClusterID = clusterID
	// The gateway already serves /metrics. Leave the protocol listen address
	// unset so startup does not claim a second server.
	metricCfg.MetricsAddr = ""
	return protocoltelemetry.NewMetrics(peerID, peerID, &metricCfg, log)
}
