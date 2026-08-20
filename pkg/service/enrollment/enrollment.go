// Package enrollment implements gateway self-enrollment against optimum-auth.
//
// Instead of a human pre-minting one ogw_ secret per host, an operator mints a
// single reusable org join credential (ojk_) and every gateway configured with it
// registers its own keypair:
//
//  1. generate a P-256 keypair, kept on disk and never sent anywhere;
//  2. POST the public JWK to /api/v1/gateways/enroll along with the join token and
//     a proof-of-possession assertion signed by the private half, receiving a
//     client_id back (no secret in the response);
//  3. mint tokens at /api/v1/auth/token with an RFC 7523 client assertion.
//
// After enrollment no shared secret crosses the wire, and the join key can be
// revoked without touching gateways already enrolled through it.
package enrollment

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	optio "github.com/getoptimum/optimum-common/pkg/io"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	// CredentialFile is the credential's name inside the enrollment directory.
	CredentialFile = "enrollment.json"

	// EnrollPath and TokenPath are appended to the auth ISSUER (not the request
	// URL) to form assertion audiences. optimum-auth derives both from
	// SIGNER_ISSUER, so anything else fails the audience check.
	EnrollPath = "/api/v1/gateways/enroll"
	TokenPath  = "/api/v1/auth/token"

	// assertionLifetime is well inside the 120s ceiling optimum-auth enforces on
	// exp - iat. The server also allows 60s of clock tolerance, so a client clock up
	// to ~119s slow still verifies; a fast clock is not bounded server-side.
	assertionLifetime = 60 * time.Second

	// AssertionValidityBudget caps a request-plus-retries sequence inside
	// assertionLifetime; the shared HTTP client has no timeout of its own.
	AssertionValidityBudget = 30 * time.Second
)

// ErrInvalidEnrollment is the 401 from the enroll endpoint. Unknown, expired, exhausted
// and revoked join keys, a cross-org thumbprint and a bad proof all collapse to one
// 401 upstream to defeat enumeration, so the cause cannot be recovered here; check
// uses_count / expires_at / status on the join key instead.
var ErrInvalidEnrollment = errors.New("enrollment: join credential rejected (401)")

// ErrEnrollmentConflict means the join key was accepted but the credential could not
// be created: this gateway's label is already live in the org, or the org is at its
// key cap. Operator action, not a retry, so treat it as terminal.
var ErrEnrollmentConflict = errors.New("enrollment: credential conflict (409)")

// PublicJWK is a P-256 public key in the exact shape optimum-auth accepts.
//
// Field order is load-bearing: encoding/json emits struct fields in declaration
// order, and the RFC 7638 thumbprint is the SHA-256 of the JSON with members in
// lexicographic order (crv, kty, x, y). Reordering these silently changes every
// thumbprint we compute.
type PublicJWK struct {
	Crv string `json:"crv"`
	Kty string `json:"kty"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// Credential is the durable result of enrolling: our keypair plus the client_id
// optimum-auth issued for it. The private key never leaves the host.
type Credential struct {
	ClientID   string    `json:"client_id"`
	Type       string    `json:"type"`
	ChainID    string    `json:"chain_id"`
	Thumbprint string    `json:"thumbprint"`
	PeerID     string    `json:"peer_id,omitempty"`
	PrivateKey []byte    `json:"private_key_pkcs8"`
	EnrolledAt time.Time `json:"enrolled_at"`

	key *ecdsa.PrivateKey
}

// Options configures a single enrollment attempt.
type Options struct {
	// Issuer is the auth service issuer, trailing slash trimmed. Assertion
	// audiences are built from it, NOT from the URL we post to: the two differ
	// whenever the worker runs somewhere other than its canonical host.
	Issuer string
	Dir    string
	// JoinKey is the raw ojk_ credential. Only needed to enroll.
	JoinKey string
	// PeerID is recorded on the enrollment audit row.
	PeerID string
	// Label identifies this host in the operator console.
	Label string
}

// NormalizeIssuer trims a trailing slash so audiences concatenate cleanly. Kept
// identical to jwks_verifier's issuer handling so both derive the same value.
func NormalizeIssuer(raw string) string { return strings.TrimRight(raw, "/") }

const coordLen = 32

// jwkFromPublic builds the canonical JWK. Coordinates come from the SEC 1
// uncompressed point because it is fixed-width; the big.Int coordinates are not,
// and a short one is rejected outright (see TestCoordinatesAreFixedWidth).
func jwkFromPublic(pub *ecdsa.PublicKey) (PublicJWK, error) {
	raw, err := pub.Bytes()
	if err != nil {
		return PublicJWK{}, fmt.Errorf("enrollment: encode public key: %w", err)
	}
	if len(raw) != 1+2*coordLen || raw[0] != 4 {
		return PublicJWK{}, fmt.Errorf("enrollment: unexpected public key encoding (%d bytes)", len(raw))
	}
	return PublicJWK{
		Crv: "P-256",
		Kty: "EC",
		X:   base64.RawURLEncoding.EncodeToString(raw[1 : 1+coordLen]),
		Y:   base64.RawURLEncoding.EncodeToString(raw[1+coordLen:]),
	}, nil
}

// Thumbprint is the RFC 7638 SHA-256 thumbprint, base64url without padding.
func (j PublicJWK) Thumbprint() (string, error) {
	canonical, err := json.Marshal(j)
	if err != nil {
		return "", fmt.Errorf("enrollment: marshal jwk: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// PublicJWK returns the credential's public key in wire shape.
func (c *Credential) PublicJWK() (PublicJWK, error) {
	if err := c.ensureKey(); err != nil {
		return PublicJWK{}, err
	}
	return jwkFromPublic(&c.key.PublicKey)
}

func (c *Credential) ensureKey() error {
	if c.key != nil {
		return nil
	}
	if len(c.PrivateKey) == 0 {
		return errors.New("enrollment: credential has no private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(c.PrivateKey)
	if err != nil {
		return fmt.Errorf("enrollment: parse private key: %w", err)
	}
	ec, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return fmt.Errorf("enrollment: private key is %T, want *ecdsa.PrivateKey", parsed)
	}
	if ec.Curve != elliptic.P256() {
		return errors.New("enrollment: private key is not P-256")
	}
	c.key = ec
	return nil
}

// SignAssertion produces an RFC 7523 client assertion for aud. peerID travels
// inside the signature: optimum-auth prefers the signed value and rejects a
// mismatch, which is what stops a replay for another peer.
func (c *Credential) SignAssertion(aud, peerID string) (string, error) {
	if err := c.ensureKey(); err != nil {
		return "", err
	}
	return signAssertion(c.key, c.ClientID, aud, peerID)
}

// signAssertion is shared by the enrollment proof (signed as the JWK thumbprint,
// the only identity we have before a client_id exists) and by token minting
// (signed as the client_id). optimum-auth requires iss == sub in both cases.
func signAssertion(key *ecdsa.PrivateKey, subject, aud, peerID string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": subject,
		"sub": subject,
		"aud": aud,
		"iat": now.Unix(),
		"exp": now.Add(assertionLifetime).Unix(),
		"jti": uuid.NewString(),
	}
	if peerID != "" {
		claims["peer_id"] = peerID
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodES256, claims).SignedString(key)
	if err != nil {
		return "", fmt.Errorf("enrollment: sign assertion: %w", err)
	}
	return signed, nil
}

type enrollRequest struct {
	JoinToken       string    `json:"join_token"`
	PublicJWK       PublicJWK `json:"public_jwk"`
	EnrollAssertion string    `json:"enroll_assertion"`
	PeerID          string    `json:"peer_id,omitempty"`
	Label           string    `json:"label,omitempty"`
}

type enrollResponse struct {
	ClientID string `json:"client_id"`
	Type     string `json:"type"`
	ChainID  string `json:"chain_id"`
	Error    string `json:"error"`
}

// terminalEnrollStatuses will not become valid on retry. 409 included: a duplicate
// label or a full org needs an operator, and retrying just re-POSTs the same body.
var terminalEnrollStatuses = []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict}

func credentialPath(dir string) string { return filepath.Join(dir, CredentialFile) }

// ensureWritable verifies the credential directory can actually be written to.
// os.MkdirAll returns nil for an existing directory whatever its mode, so the
// directory existing is not evidence that Save will succeed.
func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("enrollment: create %s: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".enroll-probe-*")
	if err != nil {
		return fmt.Errorf("enrollment: %s is not writable, refusing to enroll a credential that cannot be persisted: %w", dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name)
}

// Load reads a persisted credential. A missing file is os.ErrNotExist; any other
// error is surfaced rather than silently re-enrolling.
func Load(dir string) (*Credential, error) {
	raw, err := optio.LoadFromFile(credentialPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("enrollment: read %s: %w", credentialPath(dir), err)
	}
	var c Credential
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("enrollment: parse %s: %w", credentialPath(dir), err)
	}
	if c.ClientID == "" {
		return nil, fmt.Errorf("enrollment: %s has no client_id", credentialPath(dir))
	}
	if err := c.ensureKey(); err != nil {
		return nil, err
	}
	jwk, err := jwkFromPublic(&c.key.PublicKey)
	if err != nil {
		return nil, err
	}
	derived, err := jwk.Thumbprint()
	if err != nil {
		return nil, err
	}
	if c.Thumbprint != "" && derived != c.Thumbprint {
		return nil, fmt.Errorf(
			"enrollment: %s pairs client_id %s with a key whose thumbprint is %s, not the recorded %s",
			credentialPath(dir), c.ClientID, derived, c.Thumbprint)
	}
	return &c, nil
}

// Save persists a credential at 0600 with a CRC64 checksum, via the same helper
// the node identity uses.
func Save(dir string, c *Credential) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("enrollment: create %s: %w", dir, err)
	}
	raw, err := json.Marshal(c) //nolint:gosec // persisting the private key is the point
	if err != nil {
		return fmt.Errorf("enrollment: marshal credential: %w", err)
	}
	if err := optio.AtomicallySaveToFile(credentialPath(dir), raw); err != nil {
		return fmt.Errorf("enrollment: write %s: %w", credentialPath(dir), err)
	}
	return nil
}

// Enroll generates a keypair, registers its public half, and persists the result.
//
// The proof-of-possession is signed as the JWK thumbprint because that is the only
// identity that exists before optimum-auth issues a client_id. Its audience is the
// enroll endpoint, distinct from the token endpoint's, so an enrollment proof
// cannot be replayed to mint.
func Enroll(ctx context.Context, log logger.AppLogger, opts *Options) (*Credential, error) {
	if opts.JoinKey == "" {
		return nil, errors.New("enrollment: join key is required")
	}
	if opts.Issuer == "" {
		return nil, errors.New("enrollment: issuer is required")
	}

	if err := ensureWritable(opts.Dir); err != nil {
		return nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("enrollment: generate keypair: %w", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("enrollment: marshal private key: %w", err)
	}

	jwk, err := jwkFromPublic(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	thumbprint, err := jwk.Thumbprint()
	if err != nil {
		return nil, err
	}

	issuer := NormalizeIssuer(opts.Issuer)
	assertion, err := signAssertion(key, thumbprint, issuer+EnrollPath, "")
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, AssertionValidityBudget)
	defer cancel()

	parsed, status, err := utils.RetryPostRequest[enrollResponse](
		ctx,
		issuer+EnrollPath,
		enrollRequest{
			JoinToken:       opts.JoinKey,
			PublicJWK:       jwk,
			EnrollAssertion: assertion,
			PeerID:          opts.PeerID,
			Label:           opts.Label,
		},
		nil,
		terminalEnrollStatuses...,
	)
	// Status 0 is the only case with no response to classify. An unparseable body still
	// carries its status, and the status is what decides terminal-and-typed, so classify
	// first: otherwise a 409 behind an HTML error page degrades to an untyped failure.
	if status == 0 {
		return nil, fmt.Errorf("enrollment: POST enroll: %w", err)
	}

	switch status {
	case http.StatusOK, http.StatusCreated:
		if err != nil {
			return nil, fmt.Errorf("enrollment: POST enroll: %w", err)
		}
	case http.StatusUnauthorized:
		return nil, ErrInvalidEnrollment
	case http.StatusConflict:
		// Carry the upstream code: label_conflict and gateway_key_limit need different fixes.
		if parsed != nil && parsed.Error != "" {
			return nil, fmt.Errorf("%w: %s", ErrEnrollmentConflict, parsed.Error)
		}
		return nil, ErrEnrollmentConflict
	default:
		detail := ""
		if parsed != nil {
			detail = parsed.Error
		}
		return nil, fmt.Errorf("enrollment: enroll returned %d (error=%q)", status, detail)
	}
	if parsed == nil || parsed.ClientID == "" {
		return nil, errors.New("enrollment: enroll response missing client_id")
	}

	cred := &Credential{
		ClientID:   parsed.ClientID,
		Type:       parsed.Type,
		ChainID:    parsed.ChainID,
		Thumbprint: thumbprint,
		PeerID:     opts.PeerID,
		PrivateKey: pkcs8,
		EnrolledAt: time.Now().UTC(),
		key:        key,
	}
	if err := Save(opts.Dir, cred); err != nil {
		return nil, err
	}

	log.Info("enrolled gateway credential",
		logger.WithString("client_id", cred.ClientID),
		logger.WithString("type", cred.Type),
		logger.WithString("chain_id", cred.ChainID),
		logger.WithString("thumbprint", cred.Thumbprint),
		logger.WithString("path", credentialPath(opts.Dir)),
	)
	return cred, nil
}

// LoadOrEnroll returns the persisted credential when one exists, enrolling only on
// a genuine miss. Two processes sharing a directory would both enroll and orphan
// one credential; there is no lock because the default directory also holds the
// mumP2P identity, which they cannot share either. Enrollment is idempotent on the thumbprint upstream, but a lost
// private key means a NEW keypair, which is a new enrollment and a burnt use, so
// the on-disk credential is the thing that matters.
func LoadOrEnroll(ctx context.Context, log logger.AppLogger, opts *Options) (cred *Credential, reused bool, err error) {
	cred, err = Load(opts.Dir)
	switch {
	case err == nil:
		// The credential is bound to the peer it enrolled with. If the identity was
		// regenerated under it, every mint 401s with nothing pointing at the cause.
		if cred.PeerID != "" && opts.PeerID != "" && cred.PeerID != opts.PeerID {
			return nil, false, fmt.Errorf(
				"enrollment: %s was enrolled for peer %s but this node is %s; the mumP2P identity changed",
				credentialPath(opts.Dir), cred.PeerID, opts.PeerID)
		}
		log.Info("reusing enrolled gateway credential",
			logger.WithString("client_id", cred.ClientID),
			logger.WithString("path", credentialPath(opts.Dir)),
		)
		return cred, true, nil
	case errors.Is(err, os.ErrNotExist):
		cred, err = Enroll(ctx, log, opts)
		return cred, false, err
	default:
		return nil, false, err
	}
}
