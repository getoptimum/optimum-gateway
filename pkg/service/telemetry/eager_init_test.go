package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

// Every result child of the auth counters must exist at 0 from init, not from
// first increment.
//
// This is the difference between an alertable counter and an unalertable one. A
// CounterVec child is created on its first Inc, so a counter whose failure paths
// have never fired exports only its success child — and a rule like
// rate(auth_token_mint_total{result!="success"}[5m]) then selects ZERO series,
// which Prometheus reports as health=ok forever rather than as "no data".
//
// Measured on the live fleet 2026-10-06, before this was fixed:
//
//	auth_token_mint_total              310 series, ALL result="success"
//	p2p_handshake_cluster_claim_total  309 series, ALL result="authorized"
//	auth_enrollment_total              268 series: 267 "reused", 1 "success"
//
// Verified this test fails on the unfixed code with "got 0 children" on all
// three families, so it pins the property rather than merely describing it.
func TestAuthMetricChildrenAreEager(t *testing.T) {
	reg := prometheus.NewRegistry()
	commonmetrics.SetLabeledRegistry(reg, "testns")
	subsystem = "gw"

	initAuthMetrics()

	gathered, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	children := make(map[string]int, len(gathered))
	for _, mf := range gathered {
		children[mf.GetName()] = len(mf.GetMetric())
	}

	// Counts come from the result-constant blocks at the top of auth.go. If a
	// new result value is added there and not to the eager-init loop, this fails
	// — which is the point: a result that only appears when it first happens is
	// a result nothing can alert on.
	for name, want := range map[string]int{
		"testns_gw_auth_token_mint_total":             10,
		"testns_gw_auth_enrollment_total":             2,
		"testns_gw_p2p_handshake_cluster_claim_total": 2,
	} {
		if got := children[name]; got != want {
			t.Errorf("%s: got %d children, want %d", name, got, want)
		}
	}
}
