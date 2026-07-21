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
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-common/pkg/pointers"
	randutil "github.com/getoptimum/optimum-common/pkg/rand"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

var (
	ErrUnknownKey   = errors.New("auth_token: api key not recognized (401)")
	ErrKeyRevoked   = errors.New("auth_token: api key revoked (403)")
	ErrKeySuspended = errors.New("auth_token: api key suspended (403)")
)

const (
	mintPath  = "/api/v1/auth/token" // mintPath is appended to AppConfig.RemoteAuthURL to form the mint endpoint.
	flagsPath = "/api/v1/auth/flags" // per-gateway runtime flags, polled between re-mints.
	// Upstream issues 6h JWTs; refreshing around the 3h mark leaves a 3h
	// fence for transient auth-service outages while still hitting the
	// auth service only ~8 times/day.
	refreshIntervalMinSec = 10_500 // 2h55m
	refreshIntervalMaxSec = 11_100 // 3h05m
	// Flags poll cadence: fast enough that a propagation toggle lands in ~1-2
	// minutes, jittered so a fleet doesn't poll in lockstep.
	flagsIntervalMinSec = 55
	flagsIntervalMaxSec = 70
)

type Service struct {
	log         logger.AppLogger
	apiKey      string
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
	flagsURL       string
	// propagationSink pushes the per-key propagation flag into config so
	// PropagationEnabled() reflects it; nil on a disabled manager.
	propagationSink     func(bool)
	propagationEnabled  atomic.Bool
	flagsUnsupportedLog atomic.Bool
}

type mintResponse struct {
	AccessToken      string   `json:"access_token,omitempty"`
	ServicesToken    string   `json:"services_token,omitempty"`
	TokenType        string   `json:"token_type,omitempty"`
	ExpiresIn        int64    `json:"expires_in,omitempty"`
	OperatorID       string   `json:"operator_id,omitempty"`
	ValidatorIndexes []uint64 `json:"validator_indexes,omitempty"`
	// Pointer so a pre-rollout auth service (field absent) stays fail-open true.
	PropagationEnabled *bool  `json:"propagation_enabled,omitempty"`
	Error              string `json:"error,omitempty"`
}

type flagsResponse struct {
	PropagationEnabled *bool  `json:"propagation_enabled,omitempty"`
	Error              string `json:"error,omitempty"`
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
//	EnableAuth=false                — LOCAL DEV ONLY; disabled Manager.
//	EnableAuth=true + APIKey empty  — same: disabled Manager with an info log.
//	EnableAuth=true + APIKey set    — full path: build JWKS verifier and
//	                                  return a ready-to-mint Manager.
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
	case appCfg.APIKey == "":
		log.Info("OPT_API_KEY not set — auth_token disabled")
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

	srv := &Service{
		log:      log.With(logger.WithService("auth_token")),
		apiKey:   appCfg.APIKey,
		mintURL:  strings.TrimRight(appCfg.RemoteAuthURL, "/") + mintPath,
		flagsURL: strings.TrimRight(appCfg.RemoteAuthURL, "/") + flagsPath,
		verifier: verifier,
		mintPayload: map[string]string{
			"api_key": appCfg.APIKey,
			"peer_id": identityKey.ID.String(),
		},
		propagationSink: appCfg.SetKeyPropagationEnabled,
	}
	srv.propagationEnabled.Store(true)
	return srv, nil
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
// EnableAuth=false or OPT_API_KEY is empty). Callers that need to
// distinguish "auth not configured" from "auth configured but token bad"
// use this — most callers just call the regular methods, which degrade
// gracefully on a disabled manager.
func (m *Service) IsEnabled() bool {
	return m.apiKey != ""
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

// Start kicks off the background refresh and flags-poll loops. No-op on a
// disabled Manager.
func (m *Service) Start(ctx context.Context) {
	if !m.IsEnabled() {
		return
	}
	m.log.Info("auth_token manager started")
	go m.refreshLoop(ctx)
	go m.flagsLoop(ctx)
}

// PropagationEnabled returns the per-key flag from the most recent mint or
// flags poll. True pre-mint and on a disabled Manager (fail-open).
func (m *Service) PropagationEnabled() bool {
	if !m.IsEnabled() {
		return true
	}
	return m.propagationEnabled.Load()
}

// applyPropagation stores the flag (nil = absent field = fail-open true),
// pushes it into config and logs transitions.
func (m *Service) applyPropagation(v *bool) {
	next := v == nil || *v
	prev := m.propagationEnabled.Swap(next)
	if m.propagationSink != nil {
		m.propagationSink(next)
	}
	if prev != next {
		m.log.Info("per-key propagation flag changed", logger.WithBool("propagation_enabled", next))
	}
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

// mint hits /auth/token, verifies the response locally, and atomically
// swaps the cached token + claims + indexes.
func (m *Service) mint(ctx context.Context) (string, error) {
	parsed, statusCode, err := commonnet.PostCurl[mintResponse](ctx, m.mintURL, m.mintPayload, nil)
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

	m.token.Store(new(parsed.AccessToken))
	m.claims.Store(claims)
	m.operatorID.Store(new(parsed.OperatorID))
	m.indexes.Replace(parsed.ValidatorIndexes)
	m.applyPropagation(parsed.PropagationEnabled)

	// Cache the services token (aud=services) for centralized pushes. Verified
	// like the handshake token; if upstream omitted it (pre-split auth) or it
	// fails verify, fall back to the handshake token so pushes keep working.
	pushToken := parsed.AccessToken
	if parsed.ServicesToken != "" {
		if sClaims, verr := m.verifier.Verify(parsed.ServicesToken, jwks_verifier.AudServices); verr != nil {
			m.log.Error("services token failed local verify; using handshake token for pushes", verr)
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

	telemetry.IncAuthMintResult(telemetry.AuthMintResultSuccess)
	if claims.ExpiresAt != nil {
		telemetry.SetAuthTokenExpiresAt(claims.ExpiresAt.Unix())
	}
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

// flagsLoop polls /auth/flags between re-mints so a propagation toggle lands
// in ~1-2 minutes instead of the ~3h mint cadence. Fail-open on every error:
// the last applied value stays until a poll succeeds. A pre-rollout auth
// service (404/405) is logged once and polling continues so an upgraded
// service is picked up without a restart.
func (m *Service) flagsLoop(ctx context.Context) {
	for {
		sleepSec, _ := randutil.RandBetween(flagsIntervalMinSec, flagsIntervalMaxSec)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(sleepSec) * time.Second):
		}
		parsed, statusCode, err := commonnet.PostCurl[flagsResponse](ctx, m.flagsURL, m.mintPayload, nil)
		switch {
		case err != nil:
			m.log.Debug("flags poll failed; keeping last value", logger.WithString("err", err.Error()))
		case statusCode == http.StatusOK && parsed != nil:
			m.applyPropagation(parsed.PropagationEnabled)
		case statusCode == http.StatusNotFound || statusCode == http.StatusMethodNotAllowed:
			if m.flagsUnsupportedLog.CompareAndSwap(false, true) {
				m.log.Info("auth service does not serve /auth/flags yet; propagation toggles apply on re-mint only")
			}
		case statusCode == http.StatusUnauthorized:
			// Key no longer active; refreshLoop owns terminal handling, this loop
			// just stops flapping the flag.
			m.log.Error("flags poll unauthorized; keeping last value", ErrUnknownKey)
		default:
			m.log.Debug("flags poll unexpected status; keeping last value", logger.WithInt("status", statusCode))
		}
	}
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
				m.log.Error("api key terminal failure — refresh loop exiting", err)
				return
			default:
				m.log.Error("auth refresh failed; will retry next tick", err)
			}
		}
	}
}
