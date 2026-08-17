package jwks_verifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonjwks "github.com/getoptimum/optimum-common/pkg/jwks"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

var ErrInvalidToken = errors.New("jwks_verifier: invalid token")

const (
	jwksPath   = "/.well-known/jwks.json" // JWKS endpoint path, appended to RemoteAuthURL to form the full JWKS URL.
	signingAlg = "ES256"                  // ES256 is the only allowed signing algorithm.
)

// AudP2P is the audience for the peer-visible handshake token used at the libp2p mumP2P handshake.
// AudServices is the audience for the services token that carries operator_id to centralized services.
// AudStream is the audience for consumer block-stream JWTs (ADR-0011).
const (
	AudP2P      = "p2p"
	AudServices = "services"
	AudStream   = "stream"
)

// maxTokenLifetime caps exp - iat at the value the upstream auth service
// is documented to mint (6h, see auth_token.refreshIntervalMinSec). The
// gateway enforces the bound itself so a mis-issued long-lived token is
// rejected even if the JWKS signature and issuer both check out.
const maxTokenLifetime = 6 * time.Hour

// maxTokenLifetimeSkew is a small additive buffer on top of maxTokenLifetime
// to absorb second-precision clock differences between issuer and gateway.
const maxTokenLifetimeSkew = 60 * time.Second

// clockSkew is the symmetric tolerance for iat/nbf/exp validation. Kept
// tight on purpose — wider windows directly extend the replay window of
// already-expired or not-yet-valid tokens.
const clockSkew = 30 * time.Second

type Claims struct {
	ScopeVersion int64                      `json:"scope_version"`
	Type         commonentities.GatewayType `json:"type"`
	ChainID      string                     `json:"chain_id"`
	// ClusterIDs is the set of clusters the token authorizes (finding #707).
	// Enforced at the handshake, not here (mirrors chain_id).
	ClusterIDs []string     `json:"cluster_ids"`
	CNF        Confirmation `json:"cnf"`
	// Gateway metadata (services token only), used for metric self-labeling.
	Label           string `json:"label"`
	Region          string `json:"region"`
	ConsensusClient string `json:"consensus_client"`
	HostingProvider string `json:"hosting_provider"`
	DVT             string `json:"dvt"`
	jwt.RegisteredClaims
}

type Confirmation struct {
	PeerID string `json:"peer_id"`
}

// Verifier validates JWTs locally using a cached JWKS document.
type Verifier struct {
	cache          *commonjwks.Cache
	expectedIssuer string
}

// New constructs a Verifier and performs the initial JWKS fetch synchronously
// (with disk-cache fallback). All settings are derived from AppConfig: the
// JWKS URL is built from RemoteAuthURL + /.well-known/jwks.json;
// the issuer pinned at verify time is RemoteAuthURL verbatim.
func New(ctx context.Context, log logger.AppLogger, appCfg *config.AppConfig) (*Verifier, error) {
	if appCfg == nil {
		return nil, errors.New("jwks_verifier: AppConfig is required")
	}
	issuer := strings.TrimRight(appCfg.RemoteAuthURL, "/")
	if issuer == "" {
		return nil, errors.New("jwks_verifier: AppConfig.RemoteAuthURL is required")
	}
	cache, err := commonjwks.New(ctx, log, commonjwks.Config{
		JWKSURL:  issuer + jwksPath,
		DiskPath: appCfg.JWKSCachePath,
		Refresh:  time.Duration(appCfg.JWKSRefreshIntervalSec) * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("jwks_verifier: init JWKS cache: %w", err)
	}
	return &Verifier{cache: cache, expectedIssuer: issuer}, nil
}

// Verify performs the full local-verification pipeline:
//   - alg pinned to ES256
//   - signature via JWKS kid lookup
//   - iss matches ExpectedIssuer
//   - aud matches the expected audience (AudP2P, AudServices, or AudStream)
//   - iat is present and not in the future (with a small clockSkew tolerance)
//   - exp in future (required, with the same skew tolerance)
//   - exp - iat <= maxTokenLifetime (6h, plus a tiny skew buffer)
//   - sub non-empty
//
// All failure modes collapse to ErrInvalidToken.
func (v *Verifier) Verify(rawJWT, audience string) (*Claims, error) {
	if rawJWT == "" {
		return nil, ErrInvalidToken
	}
	// Empty audience would silently disable aud-claim enforcement.
	if strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("%w: expected audience missing", ErrInvalidToken)
	}

	var claims Claims
	token, err := jwt.ParseWithClaims(
		rawJWT,
		&claims,
		v.cache.Keyfunc,
		jwt.WithValidMethods([]string{signingAlg}),
		jwt.WithIssuer(v.expectedIssuer),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithLeeway(clockSkew),
	)
	if err != nil {
		return nil, fmt.Errorf("%w: parse token: %w", ErrInvalidToken, err)
	}
	if token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return nil, fmt.Errorf("%w: subject %q", ErrInvalidToken, claims.Subject)
	}
	if claims.IssuedAt == nil {
		return nil, fmt.Errorf("%w: issued_at missing", ErrInvalidToken)
	}
	if claims.ExpiresAt.Sub(claims.IssuedAt.Time) > maxTokenLifetime+maxTokenLifetimeSkew {
		return nil, fmt.Errorf("%w: lifetime exceeds %s", ErrInvalidToken, maxTokenLifetime)
	}
	return &claims, nil
}
