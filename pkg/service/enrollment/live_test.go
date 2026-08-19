package enrollment_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/enrollment"
)

// TestLiveEnroll drives a real enrollment against a deployed optimum-auth. It is
// skipped unless OPT_LIVE_JOIN_KEY is set, so it never runs in CI.
//
// This is the only check that exercises the cross-implementation pieces the unit
// tests can only approximate: that our RFC 7638 thumbprint matches jose's, that
// the JWK survives the Worker's 43-char coordinate validation, and that the PoP
// audience is what the Worker derives from SIGNER_ISSUER.
//
//	OPT_LIVE_JOIN_KEY=ojk_test_... \
//	OPT_LIVE_AUTH_URL=https://dev-auth.getoptimum.io \
//	go test ./pkg/service/enrollment -run TestLiveEnroll -v -count=1
//
// Each run with a fresh credential directory consumes one use of the join key.
func TestLiveEnroll(t *testing.T) {
	joinKey := os.Getenv("OPT_LIVE_JOIN_KEY")
	if joinKey == "" {
		t.Skip("set OPT_LIVE_JOIN_KEY to run against a deployed optimum-auth")
	}
	issuer := os.Getenv("OPT_LIVE_AUTH_URL")
	if issuer == "" {
		issuer = "https://dev-auth.getoptimum.io"
	}

	dir := os.Getenv("OPT_LIVE_CRED_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	// The label is unique per org among live credentials, so a fixed one enrolls
	// once and then 409s forever. Vary label and peer_id per run.
	stamp := time.Now().UnixNano()
	peerID := os.Getenv("OPT_LIVE_PEER_ID")
	if peerID == "" {
		peerID = fmt.Sprintf("16Uiu2HAmLiveEnroll%d", stamp)
	}
	label := fmt.Sprintf("live-enroll-test-%d", stamp)

	log := logger.NewAppSLogger(logger.Debug)
	cred, reused, err := enrollment.LoadOrEnroll(t.Context(), log, &enrollment.Options{
		Issuer:  enrollment.NormalizeIssuer(issuer),
		Dir:     dir,
		JoinKey: joinKey,
		PeerID:  peerID,
		Label:   label,
	})
	require.NoError(t, err)
	require.NotEmpty(t, cred.ClientID)
	t.Logf("client_id=%s type=%s chain_id=%s thumbprint=%s reused=%v",
		cred.ClientID, cred.Type, cred.ChainID, cred.Thumbprint, reused)

	// A second call must not re-enroll, so a restart never burns a join-key use.
	_, reusedAgain, err := enrollment.LoadOrEnroll(t.Context(), log, &enrollment.Options{
		Issuer: enrollment.NormalizeIssuer(issuer), Dir: dir, JoinKey: joinKey, PeerID: peerID,
	})
	require.NoError(t, err)
	require.True(t, reusedAgain)

	// The credential must be able to authenticate: mint a real token pair.
	assertion, err := cred.SignAssertion(enrollment.NormalizeIssuer(issuer)+enrollment.TokenPath, peerID)
	require.NoError(t, err)

	resp := mintLive(t, enrollment.NormalizeIssuer(issuer)+enrollment.TokenPath, map[string]string{
		"client_assertion":      assertion,
		"client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		"peer_id":               peerID,
	})
	require.NotEmpty(t, resp.AccessToken, "mint failed: %s", resp.Error)

	claims := jwt.MapClaims{}
	_, _, err = jwt.NewParser().ParseUnverified(resp.AccessToken, claims)
	require.NoError(t, err)
	t.Logf("access_token claims: sub=%v chain_id=%v type=%v cluster_ids=%v cnf=%v",
		claims["sub"], claims["chain_id"], claims["type"], claims["cluster_ids"], claims["cnf"])

	require.Equal(t, cred.ClientID, claims["sub"])

	// The claim the mumP2P handshake gates on. Empty here means the join key was
	// minted without cluster_ids, or the DB migration has not been applied: the
	// gateway would authenticate and then fail every handshake.
	clusters, _ := claims["cluster_ids"].([]any)
	require.NotEmpty(t, clusters,
		"cluster_ids is empty: this gateway would be rejected at every mesh handshake")
}
