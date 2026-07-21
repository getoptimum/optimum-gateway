package gossipsub_gateway

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/logger"
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

func (s *Service) handshakeHandler(peerID peer.ID, decoder *json.Decoder) error {
	var h Handshake
	if err := decoder.Decode(&h); err != nil {
		return err
	}
	// Non-authoritative pre-filter on the self-asserted envelope; the load-bearing
	// cluster check is on the verified JWT claim below.
	if h.ClusterID != s.cfg.GatewayClusterID {
		return fmt.Errorf("invalid cluster ID: %s", h.ClusterID)
	}
	claims, err := s.authMgr.VerifyToken(h.JWTToken)
	if err != nil {
		return fmt.Errorf("invalid JWT token: %w", err)
	}
	if claims == nil {
		if !s.cfg.EnableAuth {
			return nil
		}
		return fmt.Errorf("invalid JWT token: empty claims")
	}
	if claims.CNF.PeerID != peerID.String() {
		err = fmt.Errorf("peer ID mismatch: expected %s, got %s", peerID.String(), claims.CNF.PeerID)
		s.log.Error("got mismatch token for peer", err, logger.WithString("peer_commit_hash", h.CommitHash))
		return err
	}
	// Cluster binding (#707): reject unless this gateway's cluster is a member of the
	// verified cluster_ids claim (missing or non-member both fail).
	if len(claims.ClusterIDs) == 0 {
		telemetry.IncClusterClaimResult(telemetry.ClusterClaimRejected)
		return fmt.Errorf("missing cluster claim")
	}
	if !slices.Contains(claims.ClusterIDs, s.cfg.GatewayClusterID) {
		telemetry.IncClusterClaimResult(telemetry.ClusterClaimRejected)
		return fmt.Errorf("cluster not authorized: %s not in %v", s.cfg.GatewayClusterID, claims.ClusterIDs)
	}
	telemetry.IncClusterClaimResult(telemetry.ClusterClaimAuthorized)
	return nil
}
