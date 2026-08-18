package message_router

import (
	"context"
	"fmt"
	"time"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// Service responsible to decide should we forward message into mump2p or back to associated CL with gateway
// currently works with:
// * attestation topics
// * beacon block topics
// rest topics will be dropped
// require connection to optimum's bootstrap service
type Service struct {
	cfg     *config.AppConfig
	log     logger.AppLogger
	authMgr *auth_token.Service
	// knownValidators keep mapping between validator and target chunkID. also keep length of target chunk
	// 1st number is chunkID, 2nd number is chunk length
	knownValidators *syncx.RWMap[uint64, [2]uint64]
}

func NewService(ctx context.Context, cfg *config.AppConfig, log logger.AppLogger, authMgr *auth_token.Service) (*Service, error) {
	srv := &Service{
		cfg:             cfg,
		log:             log.With(logger.WithService("message_router")),
		authMgr:         authMgr,
		knownValidators: syncx.NewRWMap[uint64, [2]uint64](),
	}
	srv.SetKnownValidators(authMgr.ValidatorIndexes())
	go srv.bgSync(ctx)
	return srv, nil
}

// ShouldForwardMessageToMumP2P decides whether the gateway may fan out a locally-produced CL message into MumP2P
func (s *Service) ShouldForwardMessageToMumP2P(l logger.AppLogger, kind topics.TopicKind, srcTopic string, payload []byte) bool {
	if kind == topics.TopicBeaconBlock {
		return true // blindly forward all beacon blocks from any CL
	}

	// Reject if we don't have a valid token (only when auth is configured).
	if s.authMgr.IsEnabled() && !s.authMgr.HasValidToken() {
		return false
	}
	gwType := commonentities.GatewayType("")
	if c := s.authMgr.OwnClaims(); c != nil {
		gwType = c.Type
	}

	if gwType != commonentities.GatewayTypePartner { // only partners can send attestation messages
		return false
	}
	telemetry.IncAttestationEvaluated()
	attester, slot, err := utils.ParseAttestationSSZTopic(payload)
	if err != nil {
		l.Error("unable to parse attestation topic", err, logger.WithString("topic_meta", kind.String()))
		telemetry.IncAttestationDropped("parse_error")
		telemetry.IncParseSSZError(srcTopic, entities.SourceLibP2P)
		return false
	}
	diff := utils.DiffUint64(slot, chainstate.CurrentSlot(time.Now()))
	telemetry.ObserveAttestationInclusionDelay(diff)
	if diff > s.cfg.GetAttestationMaxSlotAge() {
		l.Error("attestation is too old to forward",
			fmt.Errorf("attestation slot %d is older than max slot age %d", slot, s.cfg.GetAttestationMaxSlotAge()),
			logger.WithUint64("attester", attester),
			logger.WithUint64("slot", slot),
		)
		telemetry.IncAttestationDropped("stale")
		return false
	}
	if !s.IsKnownValidator(attester) {
		telemetry.IncAttestationDropped("rejected")
		l.Error("attestation rejected because validator is not known",
			fmt.Errorf("validator %d is not in known set", attester),
			logger.WithUint64("attester", attester),
		)
		return false
	}
	telemetry.IncAttestationForwarded()
	return true
}

// IsKnownValidator returns true if the given validator index is in the known set.
func (s *Service) IsKnownValidator(attesterIndex uint64) bool {
	_, ok := s.knownValidators.Load(attesterIndex)
	return ok
}

// ResolveValidatorChunk returns the deterministic chunk assignment for a known validator.
func (s *Service) ResolveValidatorChunk(attesterIndex uint64) (chunkID, chunkSize uint64, exist bool) {
	chunk, ok := s.knownValidators.Load(attesterIndex)
	if !ok {
		return 0, 0, false
	}
	return chunk[0], chunk[1], true
}

// ShouldForwardMessageToCLP2P decides whether an inbound MumP2P message should be forwarded to the local CL libp2p peer
func (s *Service) ShouldForwardMessageToCLP2P(kind topics.TopicKind, _ []byte) bool {
	gwType := commonentities.GatewayType("")
	if c := s.authMgr.OwnClaims(); c != nil {
		gwType = c.Type
	}
	if kind == topics.TopicAttestation {
		// blindly forward all attestation topics, to any connected CL, except relay ones
		return gwType != commonentities.GatewayTypeRelay
	}

	if kind == topics.TopicBeaconBlock {
		// beacon block we forward only to authorized partners
		if s.authMgr.IsEnabled() && s.authMgr.HasValidToken() {
			return gwType == commonentities.GatewayTypePartner
		}
	}
	return false
}
