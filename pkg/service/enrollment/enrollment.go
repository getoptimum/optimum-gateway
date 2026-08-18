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
	// CredentialFile lives next to the mumP2P identity by default: the credential
	// is bound to that peer ID, and the directory is already a persistent mount.
	CredentialFile = "enrollment.json"

	// EnrollPath and TokenPath are appended to the auth ISSUER (not the request
	// URL) to form assertion audiences. optimum-auth derives both from
	// SIGNER_ISSUER, so anything else fails the audience check.
	EnrollPath = "/api/v1/gateways/enroll"
	TokenPath  = "/api/v1/auth/token"

	// assertionLifetime is well inside the 120s ceiling optimum-auth enforces on
	// exp - iat, with room for the 60s clock tolerance either side.
	assertionLifetime = 60 * time.Second
)

// ErrInvalidEnrollment is the single failure the enroll endpoint reports. Unknown,
// expired, exhausted and revoked join keys, a cross-org thumbprint and a bad proof
// all collapse to one 401 upstream to defeat enumeration, so the cause cannot be
// recovered here; check uses_count / expires_at / status on the join key instead.
var ErrInvalidEnrollment = errors.New("enrollment: join credential rejected (401)")

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

// Credential is the durable result of enrolling: the keypair we generated plus the
// client_id optimum-auth issued for it. Persisted as JSON; the private key never
// leaves the host.
type Credential struct {
	ClientID   string    `json:"client_id"`
	Type       string    `json:"type"`
	ChainID    string    `json:"chain_id"`
	Thumbprint string    `json:"thumbprint"`
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
	// Dir holds enrollment.json.
	Dir string
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

// coordLen is the fixed width of a P-256 coordinate, and the reason the SEC 1
// encoding is used below rather than the big.Int coordinates.
const coordLen = 32

// jwkFromPublic builds the canonical JWK for an in-memory public key.
//
// Coordinates come from the SEC 1 uncompressed point (0x04 || X || Y), which is
// fixed-width by construction. The big.Int coordinates would not be: X.Bytes()
// drops leading zero bytes and yields a base64url string shorter than the 43
// characters optimum-auth requires, so roughly one key in 256 would be rejected.
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

// SignAssertion produces an RFC 7523 client assertion for aud, issued as the
// credential's client_id. peerID travels inside the signature when set: optimum-auth
// prefers the signed value over the request body and rejects a mismatch, so binding
// it here is what stops a captured assertion being replayed for another peer.
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

func credentialPath(dir string) string { return filepath.Join(dir, CredentialFile) }

// Load reads a previously persisted credential. A missing file is os.ErrNotExist,
// which LoadOrEnroll treats as "enroll now"; any other error is surfaced rather
// than silently re-enrolling, because re-enrolling burns a join-key use and leaves
// an orphaned credential behind.
func Load(dir string) (*Credential, error) {
	raw, err := optio.LoadFromFile(credentialPath(dir))
	if err != nil {
		return nil, err
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
	return &c, nil
}

// Save persists a credential. AtomicallySaveToFile writes through a temp file
// created at 0600 and prefixes a CRC64 checksum, so permissions and torn-write
// detection come from the same helper the node identity uses.
func Save(dir string, c *Credential) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("enrollment: create %s: %w", dir, err)
	}
	// G117 flags the private key field by name. Persisting it is the point: the
	// gateway must prove possession of this key on every mint, and it is written
	// through AtomicallySaveToFile at 0600.
	raw, err := json.Marshal(c) //nolint:gosec // private key persistence is intentional
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

	// 400 and 401 are terminal: a malformed request or a rejected join credential
	// will not become valid on retry, and retrying a rejected join key just burns
	// rate budget against an endpoint that is deliberately opaque about why.
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
		http.StatusBadRequest,
		http.StatusUnauthorized,
	)
	if err != nil {
		return nil, fmt.Errorf("enrollment: POST enroll: %w", err)
	}

	switch status {
	case http.StatusOK, http.StatusCreated:
	case http.StatusUnauthorized:
		return nil, ErrInvalidEnrollment
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
		PrivateKey: pkcs8,
		EnrolledAt: time.Now().UTC(),
		key:        key,
	}
	if err := Save(opts.Dir, cred); err != nil {
		// The credential exists upstream but we cannot prove ownership of it after a
		// restart, and the next boot would enroll again under a new keypair. Fail
		// loudly rather than run on a credential we are about to lose.
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
// a genuine miss. reused reports which happened, so callers can tell a first boot
// from a restart. Enrollment is idempotent on the thumbprint upstream, but a lost
// private key means a NEW keypair, which is a new enrollment and a burnt use, so
// the on-disk credential is the thing that matters.
func LoadOrEnroll(ctx context.Context, log logger.AppLogger, opts *Options) (cred *Credential, reused bool, err error) {
	cred, err = Load(opts.Dir)
	switch {
	case err == nil:
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
