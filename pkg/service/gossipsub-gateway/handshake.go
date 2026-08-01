package gossipsub_gateway

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// Handshake is the mump2p mesh auth handshake (ClusterID + JWT) exchanged
// between Optimum peers. Distinct from the eth2 CL status/metadata handshake
// handled by serveHandshake in subscribe_nodes.go.
type Handshake struct {
	ClusterID  string `json:"cluster_id"`
	JWTToken   string `json:"jwt_token"`
	CommitHash string `json:"commit_hash"`
}

func NewHandshake(clusterID, jwtToken, commitHash string) *Handshake {
	return &Handshake{
		ClusterID:  clusterID,
		JWTToken:   jwtToken,
		CommitHash: commitHash,
	}
}

func (s *Service) handshakeBuilder() any {
	// Peers receive the handshake token (aud=p2p) only, never the services
	// token, which carries operator_id.
	optJWT, errJ := s.authMgr.HandshakeToken(s.ctx)
	if errJ != nil {
		s.log.Error("failed to get JWT token for handshake", errJ)
	}
	return NewHandshake(s.cfg.GatewayClusterID, optJWT, s.cfg.CommitHash)
}

func (s *Service) handshakeHandler(peerID peer.ID, decoder *json.Decoder) (mump2p.HandshakeResult, error) {
	var none mump2p.HandshakeResult
	var h Handshake
	if err := decoder.Decode(&h); err != nil {
		return none, err
	}
	// Non-authoritative pre-filter on the self-asserted envelope; the load-bearing
	// cluster check is on the verified JWT claim below.
	if h.ClusterID != s.cfg.GatewayClusterID {
		return none, fmt.Errorf("invalid cluster ID: %s", h.ClusterID)
	}
	claims, err := s.authMgr.VerifyToken(h.JWTToken)
	if err != nil {
		return none, fmt.Errorf("invalid JWT token: %w", err)
	}
	if claims == nil {
		if !s.cfg.EnableAuth {
			// No token was verified, so there is no expiry to cap a session against.
			return none, nil
		}
		return none, fmt.Errorf("invalid JWT token: empty claims")
	}
	if claims.CNF.PeerID != peerID.String() {
		err = fmt.Errorf("peer ID mismatch: expected %s, got %s", peerID.String(), claims.CNF.PeerID)
		s.log.Error("got mismatch token for peer", err, logger.WithString("peer_commit_hash", h.CommitHash))
		return none, err
	}
	// Cluster binding (#707): reject unless this gateway's cluster is a member of the
	// verified cluster_ids claim (missing or non-member both fail).
	if len(claims.ClusterIDs) == 0 {
		telemetry.IncClusterClaimResult(telemetry.ClusterClaimRejected)
		return none, fmt.Errorf("missing cluster claim")
	}
	if !slices.Contains(claims.ClusterIDs, s.cfg.GatewayClusterID) {
		telemetry.IncClusterClaimResult(telemetry.ClusterClaimRejected)
		return none, fmt.Errorf("cluster not authorized: %s not in %v", s.cfg.GatewayClusterID, claims.ClusterIDs)
	}
	telemetry.IncClusterClaimResult(telemetry.ClusterClaimAuthorized)
	// Surfaced so the datagram session this peer is about to get cannot outlive
	// the token that authorized it. Verify already rejected an expired token.
	return mump2p.HandshakeResult{TokenExpiry: tokenExpiry(claims)}, nil
}

// tokenExpiry reads the verified token's exp claim. A token without one leaves
// the zero time, which caps nothing beyond the session's own default lifetime.
func tokenExpiry(claims *jwks_verifier.Claims) time.Time {
	if claims.ExpiresAt == nil {
		return time.Time{}
	}
	return claims.ExpiresAt.Time
}
