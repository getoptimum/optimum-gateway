package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"

	commonmetrics "github.com/getoptimum/optimum-common/pkg/telemetry"
)

// Result label values for auth_token_mint_total.
const (
	AuthMintResultSuccess      = "success"
	AuthMintResultUnknownKey   = "unknown_key"   // 401 from mint endpoint
	AuthMintResultRevoked      = "revoked"       // 403 key_revoked
	AuthMintResultSuspended    = "suspended"     // 403 key_suspended
	AuthMintResultForbidden    = "forbidden"     // other 403
	AuthMintResultBadStatus    = "bad_status"    // non-2xx, non-401/403
	AuthMintResultNetworkError = "network_error" // POST failed before a status was read
	AuthMintResultEmptyToken   = "empty_token"   // 2xx but body missing access_token
	AuthMintResultVerifyFailed = "verify_failed" // minted token failed local JWKS verify
)

// Result label values for p2p_handshake_cluster_claim_total: the outcome of the
// cluster-binding check at the mumP2P handshake (#707).
const (
	ClusterClaimAuthorized = "authorized" // cluster_ids present and a member
	ClusterClaimRejected   = "rejected"   // cluster_ids missing or not a member
)

var (
	authTokenMintTotal         *prometheus.CounterVec
	authTokenExpiresAt         prometheus.Gauge
	handshakeClusterClaimTotal *prometheus.CounterVec
)

func initAuthMetrics() {
	authTokenMintTotal = commonmetrics.NewCounterVec(
		"auth_token_mint_total",
		subsystem,
		"Outcomes of gateway JWT mint attempts against the remote auth service",
		[]string{"result"},
	)
	// Updated only on successful mint. Expired when time() > value (and value > 0).
	authTokenExpiresAt = commonmetrics.NewGauge(
		"auth_token_expires_at_seconds",
		subsystem,
		"Unix timestamp at which the most recently minted gateway JWT expires (0 if never minted)",
	)
	handshakeClusterClaimTotal = commonmetrics.NewCounterVec(
		"p2p_handshake_cluster_claim_total",
		subsystem,
		"Cluster-binding check outcome at the mumP2P handshake (result=authorized|rejected)",
		[]string{"result"},
	)
}

func IncAuthMintResult(result string) {
	if enabledMetrics && authTokenMintTotal != nil {
		authTokenMintTotal.WithLabelValues(result).Inc()
	}
}

func IncClusterClaimResult(result string) {
	if enabledMetrics && handshakeClusterClaimTotal != nil {
		handshakeClusterClaimTotal.WithLabelValues(result).Inc()
	}
}

func SetAuthTokenExpiresAt(expiresAtUnix int64) {
	if enabledMetrics && authTokenExpiresAt != nil {
		authTokenExpiresAt.Set(float64(expiresAtUnix))
	}
}
