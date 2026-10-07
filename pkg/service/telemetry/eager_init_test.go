package telemetry

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

// Result children must exist at 0 from init, not from first increment: a rule
// like rate(auth_token_mint_total{result!="success"}[5m]) over an absent child
// selects zero series and reports health=ok forever. Fails on the unfixed code
// with "got 0 children".
//
// Counts come from the result-constant blocks in auth.go, so adding a result
// without adding it to the eager-init loop fails here.
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
