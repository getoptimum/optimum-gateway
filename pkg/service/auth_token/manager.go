package auth_token

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/pointers"
	randutil "github.com/getoptimum/optimum-common/pkg/rand"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

var (
	ErrUnknownKey   = errors.New("auth_token: credential not recognized (401)")
	ErrKeyRevoked   = errors.New("auth_token: credential revoked (403)")
	ErrKeySuspended = errors.New("auth_token: credential suspended (403)")
)

const (
	mintPath = "/api/v1/auth/token" // mintPath is appended to the mint base URL to form the mint endpoint.
	// Upstream issues 6h JWTs; refreshing around the 3h mark leaves a 3h
	// fence for transient auth-service outages while still hitting the
	// auth service only ~8 times/day.
	refreshIntervalMinSec = 10_500 // 2h55m
	refreshIntervalMaxSec = 11_100 // 3h05m
)

type Service struct {
	log logger.AppLogger
	// enabled is set only on the full path, so a disabled Manager stays recognizable
	// once the credential itself is no longer what distinguishes one.
	enabled     bool
	mintURL     string
	mintPayload map[string]string
	verifier    *jwks_verifier.Verifier
	token       atomic.Pointer[string]
	// servicesToken is the aud=services token used to authenticate centralized
	// HTTP/push calls (it carries operator_id). Empty when upstream auth predates
	// the two-token split; callers fall back to the handshake token.
	servicesToken  atomic.Pointer[string]
	claims         atomic.Pointer[jwks_verifier.Claims]
	servicesClaims atomic.Pointer[jwks_verifier.Claims]
	operatorID     atomic.Pointer[string]
	indexes        syncx.RWSlice[uint64]
}

type mintResponse struct {
	AccessToken      string   `json:"access_token,omitempty"`
	ServicesToken    string   `json:"services_token,omitempty"`
	TokenType        string   `json:"token_type,omitempty"`
	ExpiresIn        int64    `json:"expires_in,omitempty"`
	OperatorID       string   `json:"operator_id,omitempty"`
	ValidatorIndexes []uint64 `json:"validator_indexes,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// New always returns a non-nil Manager. When auth is off (EnableAuth=false
// or APIKey empty) the returned Manager has empty apiKey/mintURL/verifier
// and every operation degrades to a no-op: Token returns ("", nil), Start
// is a no-op, claim getters return zero values. Callers never need to
// nil-check; use IsEnabled() where the disabled-vs-misconfigured
// distinction matters (e.g. the router's token gate).
//
// Resolution:
//
//	EnableAuth=false:                  LOCAL DEV ONLY; disabled Manager.
//	EnableAuth=true, no credential:    same, disabled Manager with an info log.
//	EnableAuth=true, with a credential: full path, build the JWKS verifier and
//	                                   return a ready-to-mint Manager.
//
// A credential is either APIKey or AuthTokenURL: the latter names a local helper
// that holds this gateway's own key and mints for it.
//
// The JWKS verifier is constructed internally so the auth wiring sits in
// one place; verifier construction can be a slow network call, so it's
// skipped entirely when auth is off.
func New(ctx context.Context, log logger.AppLogger, appCfg *config.AppConfig) (*Service, error) {
	if log == nil {
		return nil, errors.New("auth_token: log is required")
	}
	if appCfg == nil {
		return nil, errors.New("auth_token: AppConfig is required")
	}
	switch {
	case !appCfg.EnableAuth:
		log.Info("OPT_ENABLE_AUTH=false — gateway JWT mint disabled; LOCAL DEV ONLY")
		return NewDisabled(log), nil
	case appCfg.APIKey == "" && appCfg.AuthTokenURL == "":
		log.Info("neither OPT_API_KEY nor OPT_AUTH_TOKEN_URL set, auth_token disabled")
		return NewDisabled(log), nil
	}
	if appCfg.RemoteAuthURL == "" {
		return nil, errors.New("auth_token: AppConfig.RemoteAuthURL is required")
	}
	verifier, err := jwks_verifier.New(ctx, log, appCfg)
	if err != nil {
		return nil, fmt.Errorf("auth_token: init JWKS verifier: %w", err)
	}

	if _, err = identity.EnsureIdentity(appCfg.IdentityMumP2PDir); err != nil {
		return nil, fmt.Errorf("auth_token: ensure identity info: %w", err)
	}
	identityKey, err := identity.ExtractIdentityFromDir(appCfg.IdentityMumP2PDir)
	if err != nil {
		return nil, fmt.Errorf("auth_token: extract identity: %w", err)
	}

	// The mint endpoint may be local; the issuer and JWKS stay remote either way,
	// so a helper cannot become something this gateway trusts.
	mintBase := appCfg.RemoteAuthURL
	if tokenURL := strings.TrimSpace(appCfg.AuthTokenURL); tokenURL != "" {
		mintBase = tokenURL
		if appCfg.APIKey != "" {
			log.Info("OPT_AUTH_TOKEN_URL is set, so OPT_API_KEY is unused")
		}
	}

	payload := map[string]string{"peer_id": identityKey.ID.String()}
	// Omitted rather than sent empty: the helper supplies its own credential.
	if appCfg.APIKey != "" && appCfg.AuthTokenURL == "" {
		payload["api_key"] = appCfg.APIKey
	}

	return &Service{
		log:         log.With(logger.WithService("auth_token")),
		enabled:     true,
		mintURL:     strings.TrimRight(mintBase, "/") + mintPath,
		verifier:    verifier,
		mintPayload: payload,
	}, nil
}

// NewDisabled returns a Manager whose every operation is a no-op. Same as
// what New returns when EnableAuth=false, but without requiring an
// AppConfig — useful for tests that need a non-nil authMgr argument but
// don't exercise the auth path.
func NewDisabled(log logger.AppLogger) *Service {
	return &Service{log: log.With(logger.WithService("auth_token"))}
}

// IsEnabled reports whether the Manager will actually mint and verify JWTs.
// Returns false for a "disabled" Manager (the one New returns when
// EnableAuth=false or no credential is configured). Callers that need to
// distinguish "auth not configured" from "auth configured but token bad"
// use this — most callers just call the regular methods, which degrade
// gracefully on a disabled manager.
func (m *Service) IsEnabled() bool {
	return m.enabled
}

// assertBoundToUs reports whether a minted token was issued for this gateway's own
// libp2p identity, which is the only thing distinguishing it from a valid token
// belonging to some other gateway.
func (m *Service) assertBoundToUs(claims *jwks_verifier.Claims) error {
	want := m.mintPayload["peer_id"]
	if want == "" {
		return nil
	}
	got := ""
	if claims != nil && claims.CNF != nil {
		got = claims.CNF.PeerID
	}
	if got != want {
		return fmt.Errorf(
			"auth_token: minted token is bound to peer %q, not this gateway's %q", got, want)
	}
	return nil
}

// Token returns the cached JWT, minting on first call. Returns ("", nil)
// on a disabled Manager — no mint attempted.
func (m *Service) Token(ctx context.Context) (string, error) {
	if !m.IsEnabled() {
		return "", nil
	}
	// A stored token is never empty (mint errors out before caching an empty
	// access_token), so an empty value means "not yet minted".
	if tok := pointers.FromPointer(m.token.Load()); tok != "" {
		return tok, nil
	}
	return m.mint(ctx)
}

// HandshakeToken returns the peer-visible handshake token (aud=p2p) for libp2p
// handshakes. Same value Token has always returned; named for intent.
func (m *Service) HandshakeToken(ctx context.Context) (string, error) {
	return m.Token(ctx)
}

// ServicesToken returns the aud=services token (carries operator_id) for
// authenticating to centralized services. Falls back to the handshake token if
// upstream auth didn't return a services token (pre-split rollout safety).
func (m *Service) ServicesToken(ctx context.Context) (string, error) {
	if !m.IsEnabled() {
		return "", nil
	}
	if tok := pointers.FromPointer(m.servicesToken.Load()); tok != "" {
		return tok, nil
	}
	// No services token cached. If a handshake token is already minted, fall
	// back to it rather than re-minting (pre-split auth omits services_token).
	if tok := pointers.FromPointer(m.token.Load()); tok != "" {
		return tok, nil
	}
	// Nothing minted yet: mint (caches both), then prefer services over handshake.
	if _, err := m.mint(ctx); err != nil {
		return "", err
	}
	if tok := pointers.FromPointer(m.servicesToken.Load()); tok != "" {
		return tok, nil
	}
	return pointers.FromPointer(m.token.Load()), nil
}

// OwnClaims returns the parsed claims of our cached JWT (nil before first
// mint or on a disabled Manager). Callers reach for individual fields
// directly — sub, type are pure passthroughs and don't warrant their own
// wrappers.
func (m *Service) OwnClaims() *jwks_verifier.Claims {
	return m.claims.Load()
}

// OperatorID returns the opaque operator ID from the most recent mint
// response (empty pre-mint or on a disabled Manager). Sourced from the
// /auth/token response body, not the JWT.
func (m *Service) OperatorID() string {
	return pointers.FromPointer(m.operatorID.Load())
}

// GatewayLabels returns the services-token gateway metadata (#74) as stream
// labels, non-empty only. Nil before first mint or on a disabled Manager.
func (m *Service) GatewayLabels() map[string]string {
	c := m.servicesClaims.Load()
	if c == nil {
		return nil
	}
	out := map[string]string{}
	for k, v := range map[string]string{
		"gateway_label":    c.Label,
		"region":           c.Region,
		"consensus_client": c.ConsensusClient,
		"hosting_provider": c.HostingProvider,
		"dvt":              c.DVT,
	} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// Chain returns the JWT's chain_id claim, normalized to the canonical name
func (m *Service) Chain() chain.Chain {
	c := m.claims.Load()
	if c == nil {
		return ""
	}

	ch, err := chain.ChainFromString(c.ChainID)
	if err != nil {
		return ""
	}
	return ch
}

// ValidatorIndexes returns a defensive copy of the indexes from the most
// recent mint response. Empty slice pre-mint or on a disabled Manager.
func (m *Service) ValidatorIndexes() []uint64 {
	return m.indexes.LoadAll()
}

// HasValidToken returns true iff a JWT has been successfully minted and its
// `exp` is still in the future. Always false on a disabled Manager.
func (m *Service) HasValidToken() bool {
	c := m.claims.Load()
	return c != nil && c.ExpiresAt != nil && time.Now().Before(c.ExpiresAt.Time)
}

// Start kicks off the background refresh loop. No-op on a disabled Manager.
func (m *Service) Start(ctx context.Context) {
	if !m.IsEnabled() {
		return
	}
	m.log.Info("auth_token manager started")
	go m.refreshLoop(ctx)
}

// VerifyToken checks that token from another gateway is valid and associated with same chain as ours.
// Used in gateway - gateway handshake ceremony
func (m *Service) VerifyToken(rawJWT string) (*jwks_verifier.Claims, error) {
	if !m.IsEnabled() {
		return nil, nil
	}
	claims, err := m.verifier.Verify(rawJWT, jwks_verifier.AudP2P)
	if err != nil {
		return nil, fmt.Errorf("auth_token: verify JWT: %w", err)
	}
	ch, err := chain.ChainFromString(claims.ChainID)
	if err != nil {
		return nil, fmt.Errorf("auth_token: parse claims: %w", err)
	}
	if ch != m.Chain() {
		return nil, fmt.Errorf("chain mismatch: expected %s, got %s", m.Chain().String(), claims.ChainID)
	}
	return claims, nil
}

// VerifyStreamToken checks a consumer block-stream JWT (aud=stream).
// No chain gate — stream authorization is audience-only in v1 (ADR-0011).
func (m *Service) VerifyStreamToken(rawJWT string) (*jwks_verifier.Claims, error) {
	if !m.IsEnabled() {
		return nil, nil
	}
	claims, err := m.verifier.Verify(rawJWT, jwks_verifier.AudStream)
	if err != nil {
		return nil, fmt.Errorf("auth_token: verify stream JWT: %w", err)
	}
	return claims, nil
}

// mint hits /auth/token, verifies the response locally, and atomically
// swaps the cached token + claims + indexes.
func (m *Service) mint(ctx context.Context) (string, error) {
	parsed, statusCode, err := utils.RetryPostRequest[mintResponse](
		ctx,
		m.mintURL,
		m.mintPayload,
		nil,
		http.StatusUnauthorized,
		http.StatusForbidden,
	)
	if err != nil {
		telemetry.IncAuthMintResult(telemetry.AuthMintResultNetworkError)
		return "", fmt.Errorf("auth mint POST: %w", err)
	}

	switch statusCode {
	case http.StatusOK:
		// proceed
	case http.StatusUnauthorized:
		telemetry.IncAuthMintResult(telemetry.AuthMintResultUnknownKey)
		return "", ErrUnknownKey
	case http.StatusForbidden:
		if parsed != nil {
			switch parsed.Error {
			case "key_revoked":
				telemetry.IncAuthMintResult(telemetry.AuthMintResultRevoked)
				return "", ErrKeyRevoked
			case "key_suspended":
				telemetry.IncAuthMintResult(telemetry.AuthMintResultSuspended)
				return "", ErrKeySuspended
			}
		}
		telemetry.IncAuthMintResult(telemetry.AuthMintResultForbidden)
		return "", fmt.Errorf("auth_token: 403 from mint (error=%q)", parsed.Error)
	default:
		telemetry.IncAuthMintResult(telemetry.AuthMintResultBadStatus)
		return "", fmt.Errorf("auth_token: mint returned %d (error=%q)", statusCode, parsed.Error)
	}

	if parsed == nil || parsed.AccessToken == "" {
		telemetry.IncAuthMintResult(telemetry.AuthMintResultEmptyToken)
		return "", errors.New("auth_token: mint response missing access_token")
	}
	claims, err := m.verifier.Verify(parsed.AccessToken, jwks_verifier.AudP2P)
	if err != nil {
		telemetry.IncAuthMintResult(telemetry.AuthMintResultVerifyFailed)
		return "", fmt.Errorf("auth_token: minted token failed local verify: %w", err)
	}
	// A token minted for another gateway is signed and valid, so only the cnf binding
	// catches it. Peers reject a mismatch at the handshake anyway; refusing it here
	// stops this gateway adopting another's sub and operator_id for its own labels
	// first. An absent binding is refused too: the request always carries peer_id, and
	// a token without cnf could not complete a handshake regardless.
	if err := m.assertBoundToUs(claims); err != nil {
		telemetry.IncAuthMintResult(telemetry.AuthMintResultVerifyFailed)
		return "", err
	}

	m.token.Store(new(parsed.AccessToken))
	m.claims.Store(claims)
	m.operatorID.Store(new(parsed.OperatorID))
	m.indexes.Replace(parsed.ValidatorIndexes)

	// Cache the services token (aud=services) for centralized pushes. Verified
	// like the handshake token; if upstream omitted it (pre-split auth) or it
	// fails verify, fall back to the handshake token so pushes keep working.
	pushToken := parsed.AccessToken
	if parsed.ServicesToken != "" {
		sClaims, verr := m.verifier.Verify(parsed.ServicesToken, jwks_verifier.AudServices)
		if verr == nil {
			verr = m.assertBoundToUs(sClaims)
		}
		if verr != nil {
			m.log.Error("services token rejected; using handshake token for pushes", verr)
			m.servicesToken.Store(new(""))
			m.servicesClaims.Store(nil)
		} else {
			m.servicesToken.Store(new(parsed.ServicesToken))
			m.servicesClaims.Store(sClaims)
			pushToken = parsed.ServicesToken
		}
	} else {
		m.servicesToken.Store(new(""))
		m.servicesClaims.Store(nil)
	}

	m.recordSuccessfulMintMetrics(claims)
	telemetry.SetPushToken(pushToken)

	m.log.Info("minted gateway JWT",
		logger.WithString("sub", claims.Subject),
		logger.WithString("chain_id", claims.ChainID),
		logger.WithString("type", claims.Type.String()),
		logger.WithString("operator_id", parsed.OperatorID),
		logger.WithInt("validator_indexes", len(parsed.ValidatorIndexes)),
	)
	return parsed.AccessToken, nil
}

// refreshLoop sleeps for a randomized interval, then re-mints. The random
// range spreads a fleet's mint requests so billing doesn't see synchronized
// spikes.
func (m *Service) refreshLoop(ctx context.Context) {
	for {
		sleepSec, _ := randutil.RandBetween(refreshIntervalMinSec, refreshIntervalMaxSec)
		time.Sleep(time.Duration(sleepSec) * time.Second)
		if _, err := m.mint(ctx); err != nil {
			switch {
			case errors.Is(err, ErrUnknownKey),
				errors.Is(err, ErrKeyRevoked),
				errors.Is(err, ErrKeySuspended):
				m.log.Error("credential terminal failure, refresh loop exiting", err)
				return
			default:
				m.log.Error("auth refresh failed; will retry next tick", err)
			}
		}
	}
}

func (m *Service) recordSuccessfulMintMetrics(claims *jwks_verifier.Claims) {
	if claims == nil || !telemetry.MetricsEnabled() {
		return
	}
	telemetry.IncAuthMintResult(telemetry.AuthMintResultSuccess)
	if claims.ExpiresAt != nil {
		telemetry.SetAuthTokenExpiresAt(claims.ExpiresAt.Unix())
	}
}

// RefreshAuthMetrics syncs auth_token_* metrics after telemetry.InitMetrics when the
// initial JWT mint completed before metrics registration.
func (m *Service) RefreshAuthMetrics() {
	if !m.IsEnabled() {
		return
	}
	m.recordSuccessfulMintMetrics(m.OwnClaims())
}
