package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every auth result child exists at 0 from init, so a rule over a failure result
// selects a real zero rather than no series.
func TestAuthMetricChildrenAreEager(t *testing.T) {
	reg := initTestMetricsRegistry(t, initAuthMetrics)
	prefix := testMetricsNamespace + "_" + testMetricsSubsystem + "_"

	check := func(name string, results []string) {
		t.Helper()
		family := prefix + name
		require.Len(t, metricFamilyByName(t, reg, family).Metric, len(results))
		for _, r := range results {
			got := metricByLabels(t, reg, family, map[string]string{labelResult: r}).GetCounter().GetValue()
			require.Zero(t, got, "%s{%s=%q}", family, labelResult, r)
		}
	}

	check("auth_token_mint_total", []string{
		AuthMintResultSuccess, AuthMintResultUnknownKey, AuthMintResultRevoked,
		AuthMintResultSuspended, AuthMintResultForbidden, AuthMintResultBadStatus,
		AuthMintResultNetworkError, AuthMintResultEmptyToken, AuthMintResultVerifyFailed,
		AuthMintResultAssertionFailed,
	})
	check("auth_enrollment_total", []string{EnrollmentResultSuccess, EnrollmentResultReused})
	check("p2p_handshake_cluster_claim_total", []string{ClusterClaimAuthorized, ClusterClaimRejected})
}
