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
	// AuthMintResultAssertionFailed is a local failure to sign the client assertion
	// (enrollment mode only): the request never left the process.
	AuthMintResultAssertionFailed = "assertion_failed"
)

// Result label values for auth_enrollment_total. "reused" is the steady state; repeated
// "success" across a fleet means credential directories are not persisting.
const (
	EnrollmentResultSuccess = "success" // enrolled and persisted a new credential
	EnrollmentResultReused  = "reused"  // loaded an existing credential from disk
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
	authEnrollmentTotal        *prometheus.CounterVec
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
	authEnrollmentTotal = commonmetrics.NewCounterVec(
		"auth_enrollment_total",
		subsystem,
		"Outcomes of resolving this gateway's own enrollment credential at startup",
		[]string{"result"},
	)
	handshakeClusterClaimTotal = commonmetrics.NewCounterVec(
		"p2p_handshake_cluster_claim_total",
		subsystem,
		"Cluster-binding check outcome at the mumP2P handshake (result=authorized|rejected)",
		[]string{"result"},
	)

	// Eagerly create every result child at 0.
	//
	// WITHOUT THIS, NONE OF THESE COUNTERS CAN BE ALERTED ON. A CounterVec child
	// is created on its first Inc, so a counter whose failure paths have never
	// fired exports only its success child. Measured across the live fleet on
	// 2026-10-06:
	//
	//   auth_token_mint_total          310 series, ALL result="success"
	//   p2p_handshake_cluster_claim_total  309 series, ALL result="authorized"
	//   auth_enrollment_total          268 series: 267 "reused", 1 "success"
	//
	// The failure paths are correctly wired -- there are ten IncAuthMintResult
	// call sites and the handshake increments ClusterClaimRejected at
	// handshake.go:78,82 -- they have simply never been reached. So any rule of
	// the form rate(..{result!="success"}) selects ZERO series, which Prometheus
	// reports as health=ok forever. That is the single defect class the alerting
	// audit (gitops#622) exists to eliminate, and it is why the Signal / auth
	// surface currently has no alerts at all despite being fully instrumented.
	//
	// Eager init makes absence impossible: a healthy fleet publishes
	// result="revoked" 0 rather than nothing, so a rate over it is a real zero
	// and an alert on it is fireable from the moment it merges.
	//
	// Cost is 14 extra series per gateway (10 + 2 + 2), all constant at 0 until
	// something happens. Against ~310 gateways that is ~4.3k series, which is
	// negligible next to the per-topic and per-peer families already exported.
	for _, r := range []string{
		AuthMintResultSuccess, AuthMintResultUnknownKey, AuthMintResultRevoked,
		AuthMintResultSuspended, AuthMintResultForbidden, AuthMintResultBadStatus,
		AuthMintResultNetworkError, AuthMintResultEmptyToken, AuthMintResultVerifyFailed,
		AuthMintResultAssertionFailed,
	} {
		authTokenMintTotal.WithLabelValues(r)
	}
	for _, r := range []string{EnrollmentResultSuccess, EnrollmentResultReused} {
		authEnrollmentTotal.WithLabelValues(r)
	}
	for _, r := range []string{ClusterClaimAuthorized, ClusterClaimRejected} {
		handshakeClusterClaimTotal.WithLabelValues(r)
	}
}

func IncAuthMintResult(result string) {
	if enabledMetrics && authTokenMintTotal != nil {
		authTokenMintTotal.WithLabelValues(result).Inc()
	}
}

func IncEnrollmentResult(result string) {
	if enabledMetrics && authEnrollmentTotal != nil {
		authEnrollmentTotal.WithLabelValues(result).Inc()
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
