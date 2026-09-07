package gossipsub_gateway

import (
	"net/http"
	"time"

	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// HealthCheck holds the individual check name -> result.
// A skipped check does not apply to this node's mode and never counts toward Failing.
type HealthCheck struct {
	Status string `json:"status"`
	Value  *int   `json:"value,omitempty"`
}

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status        string                 `json:"status"`
	GatewayID     string                 `json:"gateway_id"`
	Version       string                 `json:"version"`
	CommitHash    string                 `json:"commit_hash"`
	UptimeSeconds int64                  `json:"uptime_seconds"`
	Checks        map[string]HealthCheck `json:"checks"`
	Failing       []string               `json:"failing,omitempty"`
}

const (
	healthOK        = "ok"
	healthFail      = "fail"
	healthSkipped   = "skipped"
	healthStatusOK  = "healthy"
	healthStatusDeg = "degraded"
	blockStaleSec   = 60
)

// BuildHealthResponse evaluates all checks and returns the response (resp) and HTTP status code (httpCode: 200 or 503).
func (s *Service) BuildHealthResponse() (resp *HealthResponse, httpCode int) {
	clPeers, _, _, _ := s.GetLibP2PPeers()
	mumPeers, _, _, _ := s.GetMumP2PPeers()
	subscribedTopics := len(s.libP2PTopics.Keys())

	lastMs := s.lastBlockReceivedAt.Load()
	var lastBlockAgeSec int
	if lastMs > 0 {
		lastBlockAgeSec = int(time.Since(time.UnixMilli(lastMs)).Seconds())
	} else {
		lastBlockAgeSec = int(time.Since(s.startedAt).Seconds())
	}

	checks := map[string]HealthCheck{
		"cl_peers":           {Status: boolStatus(clPeers >= 1), Value: new(clPeers)},
		"mump2p_peers":       {Status: boolStatus(mumPeers >= 1), Value: new(mumPeers)},
		"subscribed_topics":  {Status: boolStatus(subscribedTopics >= 1), Value: new(subscribedTopics)},
		"last_block_age_sec": {Status: boolStatus(lastBlockAgeSec < blockStaleSec), Value: new(lastBlockAgeSec)},
		"cl_health":          {Status: boolStatus(telemetry.HealthCL() == 1)},
		"mump2p_health":      {Status: boolStatus(telemetry.HealthMUM() == 1)},
	}

	// Stream-only skips the CL host and ingest (see Run), so CL-derived checks can never pass.
	if s.cfg.StreamOnly {
		skipped := HealthCheck{Status: healthSkipped}
		checks["cl_peers"] = skipped
		checks["cl_health"] = skipped
		checks["subscribed_topics"] = skipped
	}

	var failing []string
	for name, c := range checks {
		if c.Status == healthFail {
			failing = append(failing, name)
		}
	}

	overall := healthStatusOK
	httpCode = http.StatusOK
	if len(failing) > 0 {
		overall = healthStatusDeg
		httpCode = http.StatusServiceUnavailable
	}

	resp = &HealthResponse{
		Status:        overall,
		GatewayID:     s.cfg.GatewayID,
		Version:       s.cfg.Version,
		CommitHash:    s.cfg.CommitHash,
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		Checks:        checks,
		Failing:       failing,
	}
	return resp, httpCode
}

func boolStatus(ok bool) string {
	if ok {
		return healthOK
	}
	return healthFail
}
