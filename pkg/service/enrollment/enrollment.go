// Package enrollment implements gateway self-enrollment: an org-wide ojk_ join key
// registers a per-host keypair, which then mints tokens by client assertion.
// See docs/adr/0013-gateway-self-enrollment.md.
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

	// PendingKeyFile holds the keypair between generating it and persisting the
	// credential it belongs to. Reusing it keeps the thumbprint stable, which is
	// what makes the server's idempotency reachable after a lost response.
	PendingKeyFile = "enrollment.key"

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

// ErrInvalidEnrollment is the enroll endpoint's 401. Every join-key rejection collapses
// into it upstream to defeat enumeration, so the cause is not recoverable here.
var ErrInvalidEnrollment = errors.New("enrollment: join credential rejected (401)")

// ErrEnrollmentConflict means the join key was accepted but the credential could not
// be created: this gateway's label is already live in the org, or the org is at its
// key cap. Operator action, not a retry, so treat it as terminal.
var ErrEnrollmentConflict = errors.New("enrollment: credential conflict (409)")

// PublicJWK is a P-256 public key in the shape optimum-auth accepts. Field order is
// the RFC 7638 canonical order and json.Marshal depends on it: reordering silently
// changes every thumbprint. Pinned by TestCanonicalJWKMemberOrder.
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
	// Issuer is both the base URL and the audience root, trailing slash trimmed. It
	// must equal the auth service's own issuer or every assertion fails the aud check.
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
	if derived != c.Thumbprint {
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
	// Fill the thumbprint here so Load can demand it: a credential without one
	// would load clean and then 401 on every mint.
	if c.Thumbprint == "" {
		jwk, err := c.PublicJWK()
		if err != nil {
			return err
		}
		if c.Thumbprint, err = jwk.Thumbprint(); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(c) //nolint:gosec // persisting the private key is the point
	if err != nil {
		return fmt.Errorf("enrollment: marshal credential: %w", err)
	}
	if err := optio.AtomicallySaveToFile(credentialPath(dir), raw); err != nil {
		return fmt.Errorf("enrollment: write %s: %w", credentialPath(dir), err)
	}
	// The writer copies the destination's mode when overwriting, so an existing
	// file at a looser mode would keep it. The private key must not inherit that.
	if err := os.Chmod(credentialPath(dir), 0o600); err != nil {
		return fmt.Errorf("enrollment: chmod %s: %w", credentialPath(dir), err)
	}
	return nil
}

// clearPendingKey drops the pending keypair. Called wherever it can no longer be
// enrolled, so a private key does not outlive its purpose on disk.
func clearPendingKey(log logger.AppLogger, dir string) {
	path := filepath.Join(dir, PendingKeyFile)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Info("could not remove the pending enrollment keypair",
			logger.WithString("path", path), logger.WithError(err))
	}
}

// pendingKey returns the keypair to enroll with, reusing the one left by a previous
// attempt so the thumbprint survives a restart. A pending key that will not load is
// replaced rather than fatal: the cost is one join-key use, not a dead gateway.
func pendingKey(log logger.AppLogger, dir string) (*ecdsa.PrivateKey, []byte, error) {
	path := filepath.Join(dir, PendingKeyFile)
	switch pkcs8, err := optio.LoadFromFile(path); {
	case err == nil:
		parsed, parseErr := x509.ParsePKCS8PrivateKey(pkcs8)
		if ec, ok := parsed.(*ecdsa.PrivateKey); parseErr == nil && ok && ec.Curve == elliptic.P256() {
			log.Info("resuming enrollment with the pending keypair", logger.WithString("path", path))
			return ec, pkcs8, nil
		}
		log.Info("pending enrollment keypair is unusable; generating a new one",
			logger.WithString("path", path), logger.WithError(parseErr))
	case !errors.Is(err, os.ErrNotExist):
		log.Info("pending enrollment keypair could not be read; generating a new one",
			logger.WithString("path", path), logger.WithError(err))
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("enrollment: generate keypair: %w", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("enrollment: marshal private key: %w", err)
	}
	// Persist before the POST. Without this a lost response strands the credential
	// upstream, and the next boot re-enrolls under a label that is already live.
	if err := optio.AtomicallySaveToFile(path, pkcs8); err != nil {
		return nil, nil, fmt.Errorf("enrollment: write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, nil, fmt.Errorf("enrollment: chmod %s: %w", path, err)
	}
	return key, pkcs8, nil
}

// Enroll generates a keypair, registers its public half, and persists the result. The
// proof is signed as the thumbprint, the only identity there is before a client_id.
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
	key, pkcs8, err := pendingKey(log, opts.Dir)
	if err != nil {
		return nil, err
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
	// Classify on status before the error: an unparseable body keeps its status, so a
	// 409 behind an HTML page stays typed. A body that fails mid-read reports status 0
	// and cannot be classified at all.
	if status == 0 {
		return nil, fmt.Errorf("enrollment: POST enroll: %w", err)
	}
	// Drop the keypair only where nothing can have been committed with it: a 400 is
	// refused before the database, and a 409 is refused at the insert, which is only
	// reached when the thumbprint matched nothing. A 401 is raised for an unknown
	// join key before that lookup, so the key is kept in case a credential exists.
	if status == http.StatusBadRequest || status == http.StatusConflict {
		clearPendingKey(log, opts.Dir)
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
	clearPendingKey(log, opts.Dir)

	log.Info("enrolled gateway credential",
		logger.WithString("client_id", cred.ClientID),
		logger.WithString("type", cred.Type),
		logger.WithString("chain_id", cred.ChainID),
		logger.WithString("thumbprint", cred.Thumbprint),
		logger.WithString("path", credentialPath(opts.Dir)),
	)
	return cred, nil
}

// LoadOrEnroll returns the persisted credential when one exists, enrolling only on a
// genuine miss. Concurrency and lost-credential trade-offs: see ADR-0013.
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
		// A credential makes any pending key stale. Left in place it would be
		// resubmitted if the credential were ever deleted to force re-enrollment,
		// and upstream idempotency would hand back the old one.
		clearPendingKey(log, opts.Dir)
		// chain_id and type come from the credential, not the configured join key:
		// a stale credential keeps its own and nothing else would reveal it.
		log.Info("reusing enrolled gateway credential",
			logger.WithString("client_id", cred.ClientID),
			logger.WithString("type", cred.Type),
			logger.WithString("chain_id", cred.ChainID),
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
