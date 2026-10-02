package gossipsub_gateway

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p"
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

// fullCapability is the admission every peer got before role-derived capabilities existed.
var fullCapability = mum_p2p.PeerCapability{CanPublish: true}

func (s *Service) handshakeHandler(peerID peer.ID, decoder *json.Decoder) (mum_p2p.PeerCapability, error) {
	var h Handshake
	if err := decoder.Decode(&h); err != nil {
		return mum_p2p.PeerCapability{}, err
	}
	// Non-authoritative pre-filter on the self-asserted envelope; the load-bearing
	// cluster check is on the verified JWT claim below.
	if h.ClusterID != s.cfg.GatewayClusterID {
		return mum_p2p.PeerCapability{}, fmt.Errorf("invalid cluster ID: %s", h.ClusterID)
	}
	claims, err := s.authMgr.VerifyToken(h.JWTToken)
	if err != nil {
		return mum_p2p.PeerCapability{}, fmt.Errorf("invalid JWT token: %w", err)
	}
	if claims == nil {
		if !s.cfg.EnableAuth {
			return fullCapability, nil
		}
		return mum_p2p.PeerCapability{}, fmt.Errorf("invalid JWT token: empty claims")
	}
	gotPeerID := ""
	if claims.CNF != nil {
		gotPeerID = claims.CNF.PeerID
	}
	if gotPeerID != peerID.String() {
		err = fmt.Errorf("peer ID mismatch: expected %s, got %s", peerID.String(), gotPeerID)
		s.log.Error("got mismatch token for peer", err, logger.WithString("peer_commit_hash", h.CommitHash))
		return mum_p2p.PeerCapability{}, err
	}
	// Cluster binding (#707): reject unless this gateway's cluster is a member of the
	// verified cluster_ids claim (missing or non-member both fail).
	if len(claims.ClusterIDs) == 0 {
		telemetry.IncClusterClaimResult(telemetry.ClusterClaimRejected)
		return mum_p2p.PeerCapability{}, fmt.Errorf("missing cluster claim")
	}
	if !slices.Contains(claims.ClusterIDs, s.cfg.GatewayClusterID) {
		telemetry.IncClusterClaimResult(telemetry.ClusterClaimRejected)
		return mum_p2p.PeerCapability{}, fmt.Errorf(
			"cluster not authorized: %s not in %v", s.cfg.GatewayClusterID, claims.ClusterIDs)
	}
	telemetry.IncClusterClaimResult(telemetry.ClusterClaimAuthorized)

	capability := mum_p2p.PeerCapability{CanPublish: claims.CanPublish()}
	if !capability.CanPublish {
		s.log.Info("admitting peer read-only",
			logger.WithPeerID(peerID),
			logger.WithString("gateway_type", claims.Type.String()),
			logger.WithString("scope", claims.Scope),
		)
	}
	return capability, nil
}
