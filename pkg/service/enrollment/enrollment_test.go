package enrollment_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/enrollment"
)

// testJoinKey is the raw ojk_ credential every stubbed enrollment presents.
const testJoinKey = "ojk_test_secret"

func testLogger() logger.AppLogger { return logger.NewAppSLogger(logger.Debug) }

// publicKeyFromJWK is the verification side of what the gateway sends, mirroring
// what optimum-auth does with the submitted key.
func publicKeyFromJWK(t *testing.T, j enrollment.PublicJWK) *ecdsa.PublicKey {
	t.Helper()
	x, err := base64.RawURLEncoding.DecodeString(j.X)
	require.NoError(t, err)
	y, err := base64.RawURLEncoding.DecodeString(j.Y)
	require.NoError(t, err)
	// ParseUncompressedPublicKey also checks the point is on the curve, which is
	// what optimum-auth's importJWK does before verifying anything.
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append([]byte{4}, append(x, y...)...))
	require.NoError(t, err)
	return pub
}

// TestThumbprintMatchesJose pins the RFC 7638 thumbprint against a value computed
// by jose's calculateJwkThumbprint, the implementation optimum-auth uses. A drift
// here means every enrollment is rejected, so the fixture is deliberately a
// hardcoded cross-implementation constant rather than a recomputation.
func TestThumbprintMatchesJose(t *testing.T) {
	jwk := enrollment.PublicJWK{
		Crv: "P-256",
		Kty: "EC",
		X:   "f83OJ3D2xF1Bg8vub9tLe1gHMzV76e8Tus9uPHvRVEU",
		Y:   "x_FEzRu9m36HLN_tue659LNpXW6pCyStikYjKIWI5a0",
	}
	got, err := jwk.Thumbprint()
	require.NoError(t, err)
	require.Equal(t, "oKIywvGUpTVTyxMQ3bwIIeQUudfr_CkLMjCE19ECD-U", got)
}

// TestCanonicalJWKMemberOrder guards the struct field order the thumbprint depends
// on: RFC 7638 hashes the members in lexicographic order.
func TestCanonicalJWKMemberOrder(t *testing.T) {
	raw, err := json.Marshal(enrollment.PublicJWK{Crv: "P-256", Kty: "EC", X: "xx", Y: "yy"})
	require.NoError(t, err)
	require.JSONEq(t, `{"crv":"P-256","kty":"EC","x":"xx","y":"yy"}`, string(raw))
	require.Equal(t, `{"crv":"P-256","kty":"EC","x":"xx","y":"yy"}`, string(raw), "member order is load-bearing")
}

// TestCoordinatesAreFixedWidth is the regression test for the leading-zero trap:
// big.Int.Bytes() drops leading zero bytes, which yields a coordinate shorter than
// the 43 base64url characters optimum-auth requires. Roughly 1 key in 256 has a
// zero high byte, so without fixed-width padding enrollment fails intermittently.
func TestCoordinatesAreFixedWidth(t *testing.T) {
	var withLeadingZero *ecdsa.PrivateKey
	for range 4000 {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		raw, err := key.PublicKey.Bytes()
		require.NoError(t, err)
		// raw is 0x04 || X || Y; a zero high byte of X is the case that used to
		// produce a short coordinate.
		if raw[1] == 0 {
			withLeadingZero = key
			break
		}
	}
	require.NotNil(t, withLeadingZero, "no key with a short X in 4000 tries")

	cred := credentialFor(t, withLeadingZero)
	jwk, err := cred.PublicJWK()
	require.NoError(t, err)
	require.Len(t, jwk.X, 43, "X must stay 43 base64url chars even with a leading zero byte")
	require.Len(t, jwk.Y, 43)
}

// credentialFor builds a Credential around an existing key by round-tripping it
// through Save/Load, which is the only supported way to construct one.
func credentialFor(t *testing.T, key *ecdsa.PrivateKey) *enrollment.Credential {
	t.Helper()
	dir := t.TempDir()
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, enrollment.Save(dir, &enrollment.Credential{
		ClientID:   "ag_test",
		PrivateKey: pkcs8,
	}))
	cred, err := enrollment.Load(dir)
	require.NoError(t, err)
	return cred
}

func TestSignAssertionClaims(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cred := credentialFor(t, key)

	raw, err := cred.SignAssertion("https://auth.test/api/v1/auth/token", "12D3KooWpeer")
	require.NoError(t, err)

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
	require.NoError(t, err)
	require.Equal(t, "ES256", parsed.Method.Alg(), "ES256 only; anything else is rejected upstream")

	require.Equal(t, "ag_test", claims["iss"])
	require.Equal(t, "ag_test", claims["sub"], "optimum-auth requires iss == sub")
	require.Equal(t, "https://auth.test/api/v1/auth/token", claims["aud"])
	require.Equal(t, "12D3KooWpeer", claims["peer_id"])
	require.NotEmpty(t, claims["jti"], "jti is a required claim")

	iat, exp := int64(claims["iat"].(float64)), int64(claims["exp"].(float64))
	require.LessOrEqual(t, exp-iat, int64(120), "optimum-auth caps assertion lifetime at 120s")
	require.Greater(t, exp-iat, int64(0))
}

func TestSignAssertionIsFreshEachCall(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cred := credentialFor(t, key)

	seen := map[string]bool{}
	for range 5 {
		raw, err := cred.SignAssertion("https://auth.test/x", "")
		require.NoError(t, err)
		claims := jwt.MapClaims{}
		_, err = jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
		require.NoError(t, err)
		jti, _ := claims["jti"].(string)
		require.NotEmpty(t, jti)
		require.False(t, seen[jti], "jti must be unique per assertion")
		seen[jti] = true
	}
}

func TestSignAssertionOmitsEmptyPeerID(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	cred := credentialFor(t, key)

	raw, err := cred.SignAssertion("https://auth.test/x", "")
	require.NoError(t, err)
	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return &key.PublicKey, nil })
	require.NoError(t, err)
	_, present := claims["peer_id"]
	require.False(t, present, "an empty peer_id must be omitted, not sent as \"\"")
}

func TestSaveUsesOwnerOnlyPermissions(t *testing.T) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, enrollment.Save(dir, &enrollment.Credential{ClientID: "ag_1", PrivateKey: pkcs8}))

	info, err := os.Stat(filepath.Join(dir, enrollment.CredentialFile))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the file holds a private key")
}

func TestLoadMissingIsNotExist(t *testing.T) {
	_, err := enrollment.Load(t.TempDir())
	require.ErrorIs(t, err, os.ErrNotExist, "a miss must be distinguishable so LoadOrEnroll can enroll")
}

// stubAuth stands in for optimum-auth's enroll endpoint, verifying the
// proof-of-possession the same way the Worker does.
type stubAuth struct {
	server *httptest.Server
	calls  atomic.Int32
	status int
	body   []byte
	seen   struct {
		jwk     enrollment.PublicJWK
		peerID  string
		label   string
		token   string
		rawBody []byte
	}
}

func newStubAuth(t *testing.T) *stubAuth {
	t.Helper()
	s := &stubAuth{}
	mux := http.NewServeMux()
	mux.HandleFunc(enrollment.EnrollPath, func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req struct {
			JoinToken       string               `json:"join_token"`
			PublicJWK       enrollment.PublicJWK `json:"public_jwk"`
			EnrollAssertion string               `json:"enroll_assertion"`
			PeerID          string               `json:"peer_id"`
			Label           string               `json:"label"`
			Extra           map[string]any       `json:"-"`
		}
		require.NoError(t, json.Unmarshal(raw, &req))
		s.seen.jwk, s.seen.peerID, s.seen.label, s.seen.token = req.PublicJWK, req.PeerID, req.Label, req.JoinToken
		s.seen.rawBody = raw

		if s.status != 0 {
			w.WriteHeader(s.status)
			_, _ = w.Write(s.body)
			return
		}

		// Verify the PoP against the SUBMITTED key, bound to this endpoint and to
		// the key's own thumbprint, exactly as the Worker does.
		thumb, err := req.PublicJWK.Thumbprint()
		require.NoError(t, err)
		claims := jwt.MapClaims{}
		_, err = jwt.ParseWithClaims(req.EnrollAssertion, claims,
			func(*jwt.Token) (any, error) { return publicKeyFromJWK(t, req.PublicJWK), nil },
			jwt.WithValidMethods([]string{"ES256"}),
			jwt.WithAudience(s.server.URL+enrollment.EnrollPath),
		)
		require.NoError(t, err, "proof-of-possession must verify against the submitted key")
		require.Equal(t, thumb, claims["sub"], "PoP sub must be the JWK thumbprint")
		require.Equal(t, thumb, claims["iss"], "PoP iss must equal sub")

		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"client_id":"ag_abc","type":"hermes","chain_id":"560048"}`))
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func TestEnrollHappyPath(t *testing.T) {
	auth := newStubAuth(t)
	dir := t.TempDir()

	cred, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer:  auth.server.URL,
		Dir:     dir,
		JoinKey: testJoinKey,
		PeerID:  "12D3KooWpeer",
		Label:   "hermes-node-1",
	})
	require.NoError(t, err)
	require.Equal(t, "ag_abc", cred.ClientID)
	require.Equal(t, "hermes", cred.Type)
	require.Equal(t, "560048", cred.ChainID)

	require.Equal(t, testJoinKey, auth.seen.token)
	require.Equal(t, "12D3KooWpeer", auth.seen.peerID)
	require.Equal(t, "hermes-node-1", auth.seen.label)
	require.Equal(t, "P-256", auth.seen.jwk.Crv)
	require.Equal(t, "EC", auth.seen.jwk.Kty)
	require.Len(t, auth.seen.jwk.X, 43)
	require.Len(t, auth.seen.jwk.Y, 43)

	// persisted and immediately usable
	reloaded, err := enrollment.Load(dir)
	require.NoError(t, err)
	require.Equal(t, cred.ClientID, reloaded.ClientID)
	require.Equal(t, cred.Thumbprint, reloaded.Thumbprint)
	_, err = reloaded.SignAssertion("https://auth.test/x", "")
	require.NoError(t, err)
}

func TestEnrollNeverSendsPrivateMaterial(t *testing.T) {
	auth := newStubAuth(t)
	cred, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer: auth.server.URL, Dir: t.TempDir(), JoinKey: testJoinKey, PeerID: "12D3KooWpeer",
	})
	require.NoError(t, err)

	// Scan the bytes actually sent. Re-marshaling the decoded struct proves nothing:
	// PublicJWK has four fields and none can hold private material.
	body := auth.seen.rawBody
	require.NotEmpty(t, body)

	// The request carries exactly these fields, so any added one fails here wherever
	// it sits in the object, not just inside public_jwk.
	var probe map[string]any
	require.NoError(t, json.Unmarshal(body, &probe))
	keys := slices.Sorted(maps.Keys(probe))
	require.Equal(t, []string{"enroll_assertion", "join_token", "peer_id", "public_jwk"}, keys)
	jwk, ok := probe["public_jwk"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, []string{"crv", "kty", "x", "y"}, slices.Sorted(maps.Keys(jwk)))

	// And the key material itself must not appear under any encoding a marshaller
	// might use. encoding/json renders []byte as padded standard base64.
	for name, enc := range map[string]string{
		"std base64":    base64.StdEncoding.EncodeToString(cred.PrivateKey),
		"rawurl base64": base64.RawURLEncoding.EncodeToString(cred.PrivateKey),
		"hex":           hex.EncodeToString(cred.PrivateKey),
	} {
		require.NotContains(t, string(body), enc, "PKCS8 key leaked as %s", name)
	}
}

// Enrolling and then failing to persist leaves a credential upstream that this node
// can never present, and a crashlooping container repeats it every restart, draining
// the join key's uses. The directory existing is not evidence it is writable, so the
// check has to be a real probe and it has to happen before the POST.
func TestEnrollRefusesAnUnwritableDirBeforeContactingTheServer(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; mode bits do not deny access")
	}
	auth := newStubAuth(t)
	dir := filepath.Join(t.TempDir(), "readonly")
	require.NoError(t, os.Mkdir(dir, 0o500))

	_, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer: auth.server.URL, Dir: dir, JoinKey: testJoinKey,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not writable")
	require.EqualValues(t, 0, auth.calls.Load(),
		"the server must not be asked for a credential we cannot store")
}

// A credential file pairing a client_id with the wrong key would otherwise look
// healthy at boot and fail three hours later as an opaque 401.
func TestLoadRejectsAThumbprintMismatch(t *testing.T) {
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, enrollment.Save(dir, &enrollment.Credential{
		ClientID:   "ag_1",
		Thumbprint: "not-the-thumbprint-of-this-key",
		PrivateKey: pkcs8,
	}))

	_, err = enrollment.Load(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not the recorded")
}

func TestEnrollRejectedJoinKey(t *testing.T) {
	auth := newStubAuth(t)
	auth.status = http.StatusUnauthorized
	auth.body = []byte(`{"error":"invalid_enrollment"}`)

	dir := t.TempDir()
	_, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer: auth.server.URL, Dir: dir, JoinKey: "ojk_test_bad",
	})
	require.ErrorIs(t, err, enrollment.ErrInvalidEnrollment)
	require.EqualValues(t, 1, auth.calls.Load(), "a rejected join key must not be retried")

	_, statErr := os.Stat(filepath.Join(dir, enrollment.CredentialFile))
	require.ErrorIs(t, statErr, os.ErrNotExist, "a failed enrollment must not leave a credential behind")
}

func TestEnrollLabelConflictIsTerminal(t *testing.T) {
	auth := newStubAuth(t)
	auth.status = http.StatusConflict
	auth.body = []byte(`{"error":"label_conflict"}`)

	dir := t.TempDir()
	_, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer: auth.server.URL, Dir: dir, JoinKey: "ojk_test_dup", Label: "node-1",
	})
	require.ErrorIs(t, err, enrollment.ErrEnrollmentConflict)
	require.Contains(t, err.Error(), "label_conflict", "the operator needs the upstream code to know what to fix")
	require.EqualValues(t, 1, auth.calls.Load(), "a conflict must not be retried")

	_, statErr := os.Stat(filepath.Join(dir, enrollment.CredentialFile))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestEnrollConflictVariants(t *testing.T) {
	// A 409 must stay typed however the body arrives: an unparseable one still carries
	// its status.
	for _, tc := range []struct {
		name, body, wantDetail string
	}{
		{"cap", `{"error":"gateway_key_limit"}`, "gateway_key_limit"},
		{"non-JSON body", "<html>409</html>", ""},
		{"empty body", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auth := newStubAuth(t)
			auth.status = http.StatusConflict
			auth.body = []byte(tc.body)

			_, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
				Issuer: auth.server.URL, Dir: t.TempDir(), JoinKey: "ojk_test_x", Label: "node-1",
			})
			require.ErrorIs(t, err, enrollment.ErrEnrollmentConflict)
			require.EqualValues(t, 1, auth.calls.Load(), "a conflict must not be retried")
			if tc.wantDetail != "" {
				require.Contains(t, err.Error(), tc.wantDetail)
			} else {
				require.Equal(t, enrollment.ErrEnrollmentConflict.Error(), err.Error(),
					"no detail means no dangling separator")
			}
		})
	}
}

func TestEnrollServerError(t *testing.T) {
	auth := newStubAuth(t)
	auth.status = http.StatusInternalServerError
	auth.body = []byte(`{"error":"internal_error"}`)

	_, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer: auth.server.URL, Dir: t.TempDir(), JoinKey: testJoinKey,
	})
	require.Error(t, err)
	require.NotErrorIs(t, err, enrollment.ErrInvalidEnrollment, "a 500 is not a credential rejection")
}

func TestLoadOrEnrollReusesWithoutCallingAuth(t *testing.T) {
	auth := newStubAuth(t)
	dir := t.TempDir()
	opts := &enrollment.Options{
		Issuer: auth.server.URL, Dir: dir, JoinKey: testJoinKey, PeerID: "12D3KooWpeer",
	}

	first, reused, err := enrollment.LoadOrEnroll(t.Context(), testLogger(), opts)
	require.NoError(t, err)
	require.False(t, reused)
	require.EqualValues(t, 1, auth.calls.Load())

	second, reused, err := enrollment.LoadOrEnroll(t.Context(), testLogger(), opts)
	require.NoError(t, err)
	require.True(t, reused)
	require.EqualValues(t, 1, auth.calls.Load(), "a restart must not burn another join-key use")
	require.Equal(t, first.ClientID, second.ClientID)
	require.Equal(t, first.Thumbprint, second.Thumbprint)
}

func TestLoadOrEnrollSurfacesCorruptCredential(t *testing.T) {
	auth := newStubAuth(t)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, enrollment.CredentialFile), []byte("not json"), 0o600))

	_, _, err := enrollment.LoadOrEnroll(t.Context(), testLogger(), &enrollment.Options{
		Issuer: auth.server.URL, Dir: dir, JoinKey: testJoinKey,
	})
	require.Error(t, err)
	require.EqualValues(t, 0, auth.calls.Load(),
		"a damaged credential must not silently re-enroll: that burns a use and orphans the old one")
}

func TestNormalizeIssuerTrimsTrailingSlash(t *testing.T) {
	require.Equal(t, "https://auth.test", enrollment.NormalizeIssuer("https://auth.test/"))
	require.Equal(t, "https://auth.test", enrollment.NormalizeIssuer("https://auth.test"))
}

func TestEnrollRequiresJoinKeyAndIssuer(t *testing.T) {
	_, err := enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{Issuer: "https://a", Dir: t.TempDir()})
	require.Error(t, err)
	_, err = enrollment.Enroll(t.Context(), testLogger(), &enrollment.Options{Dir: t.TempDir(), JoinKey: "ojk_x"})
	require.Error(t, err)
}

// The credential is bound to the peer it enrolled with. A regenerated identity
// under a surviving credential otherwise 401s at every mint with no local signal.
func TestLoadOrEnrollRejectsAChangedPeerIdentity(t *testing.T) {
	auth := newStubAuth(t)
	dir := t.TempDir()
	opts := &enrollment.Options{Issuer: auth.server.URL, Dir: dir, JoinKey: testJoinKey, PeerID: "12D3KooWfirst"}

	_, _, err := enrollment.LoadOrEnroll(t.Context(), testLogger(), opts)
	require.NoError(t, err)

	moved := *opts
	moved.PeerID = "12D3KooWsecond"
	_, _, err = enrollment.LoadOrEnroll(t.Context(), testLogger(), &moved)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mumP2P identity changed")
	require.EqualValues(t, 1, auth.calls.Load(), "must not silently re-enroll")
}
