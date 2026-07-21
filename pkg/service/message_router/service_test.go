package message_router_test

import (
	"bytes"
	"encoding/hex"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/rand"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const validAttestationHex = "f001043900090108142c1305090828672a0508110188a50e909e4f2f3180e8a78ba7ea60af387a87ebc52d07158192d7ba6a17220868385301052b807b2afe8ba3d6883be57de5b23d5ac8482e15d87f1d74c24ef4b5a8180b69e555390d28f07fb153c49dd8db8256cb4bdb40753c00e18417035907ab21eda28fa158bae2ef7da737251c3ae8061d92151afb9f15ac5aba4044c96b8afe844ceb6cbc564daf17a51c81249610bf49209dfb26e6ea74f5045dc1a7f5c2fc15d2767985c206132e22f09b1f993c8d35d12cb520a77982e9115d67ff5ba8f40110763a18c0c10c61"

const (
	testBeaconBlockTopic       = "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	testBeaconAttestationTopic = "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy"
)

func TestService_ShouldForwardMessageToMumP2P(t *testing.T) {
	freshSlot := chainstate.CurrentSlot(time.Now())
	staleSlot := freshSlot
	if staleSlot > 10 {
		staleSlot -= 10
	} else {
		staleSlot = 0
	}

	freshPayload := buildAttestationPayload(t, 42, freshSlot)
	otherValidatorPayload := buildAttestationPayload(t, 43, freshSlot)
	stalePayload := buildAttestationPayload(t, 42, staleSlot)
	log := logger.NewAppSLogger(logger.Debug)

	tests := map[string]struct {
		service *message_router.Service
		topic   string
		payload []byte
		want    bool
	}{
		"partner forwards beacon block": {
			service: newTestService(t, commonentities.GatewayTypePartner),
			topic:   testBeaconBlockTopic,
			payload: []byte("ignored"),
			want:    true,
		},
		"hermes blocks beacon block": {
			service: newTestService(t, commonentities.GatewayTypeHermes),
			topic:   testBeaconBlockTopic,
			payload: []byte("ignored"),
			want:    true,
		},
		"relay blocks beacon block": {
			service: newTestService(t, commonentities.GatewayTypeRelay),
			topic:   testBeaconBlockTopic,
			payload: []byte("ignored"),
			want:    true,
		},
		"non attestation topic is blocked": {
			service: newTestService(t, commonentities.GatewayTypePartner),
			topic:   "/eth2/c6ecb76c/sync_committee_contribution_and_proof/ssz_snappy",
			payload: []byte("ignored"),
			want:    false,
		},
		"aggregate and proof topic is blocked": {
			service: newTestService(t, commonentities.GatewayTypePartner),
			topic:   "/eth2/c6ecb76c/beacon_aggregate_and_proof/ssz_snappy",
			payload: []byte("ignored"),
			want:    false,
		},
		"known validator with fresh slot forwards": {
			service: newTestService(t, commonentities.GatewayTypePartner, 42),
			topic:   testBeaconAttestationTopic,
			payload: buildAttestationPayload(t, 42, chainstate.CurrentSlot(time.Now())),
			want:    true,
		},
		"unknown validator is blocked": {
			service: newTestService(t, commonentities.GatewayTypePartner, 42),
			topic:   testBeaconAttestationTopic,
			payload: otherValidatorPayload,
			want:    false,
		},
		"known validator with stale slot is blocked": {
			service: newTestService(t, commonentities.GatewayTypePartner, 42),
			topic:   testBeaconAttestationTopic,
			payload: stalePayload,
			want:    false,
		},
		"malformed attestation payload is blocked": {
			service: newTestService(t, commonentities.GatewayTypePartner, 42),
			topic:   testBeaconAttestationTopic,
			payload: []byte("not-snappy"),
			want:    false,
		},
		"empty validator cache blocks attestation": {
			service: newTestService(t, commonentities.GatewayTypePartner),
			topic:   testBeaconAttestationTopic,
			payload: freshPayload,
			want:    false,
		},
		"hermes blocks attestation fanout": {
			service: newTestService(t, commonentities.GatewayTypeHermes, 42),
			topic:   testBeaconAttestationTopic,
			payload: freshPayload,
			want:    false,
		},
		"relay blocks attestation fanout": {
			service: newTestService(t, commonentities.GatewayTypeRelay, 42),
			topic:   testBeaconAttestationTopic,
			payload: freshPayload,
			want:    false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, tc.service.ShouldForwardMessageToMumP2P(log, topics.ParseTopicMeta(tc.topic).Kind, tc.topic, tc.payload))
		})
	}
}

func TestService_ShouldForwardMessageToCLP2P(t *testing.T) {
	tests := map[string]struct {
		service     *message_router.Service
		topic       string
		wantForward bool
	}{
		"partner forwards attestation": {
			service:     newTestService(t, commonentities.GatewayTypePartner),
			topic:       testBeaconAttestationTopic,
			wantForward: true,
		},
		"partner forwards beacon block": {
			service:     newTestService(t, commonentities.GatewayTypePartner),
			topic:       testBeaconBlockTopic,
			wantForward: true,
		},
		"partner blocks unsupported topic": {
			service:     newTestService(t, commonentities.GatewayTypePartner),
			topic:       "/eth2/c6ecb76c/beacon_aggregate_and_proof/ssz_snappy",
			wantForward: false,
		},
		"hermes forwards attestation": {
			service:     newTestService(t, commonentities.GatewayTypeHermes),
			topic:       testBeaconAttestationTopic,
			wantForward: true,
		},
		"hermes forwards beacon block": {
			service:     newTestService(t, commonentities.GatewayTypeHermes),
			topic:       testBeaconBlockTopic,
			wantForward: false,
		},
		"relay blocks attestation": {
			service:     newTestService(t, commonentities.GatewayTypeRelay),
			topic:       testBeaconAttestationTopic,
			wantForward: false,
		},
		"relay blocks beacon block": {
			service:     newTestService(t, commonentities.GatewayTypeRelay),
			topic:       testBeaconBlockTopic,
			wantForward: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.wantForward, tc.service.ShouldForwardMessageToCLP2P(topics.ParseTopicMeta(tc.topic).Kind, nil))
		})
	}
}

func TestService_SetKnownValidatorsReplacesPreviousSet(t *testing.T) {
	service := newTestService(t, commonentities.GatewayTypePartner, 42)

	require.True(t, service.IsKnownValidator(42))
	require.False(t, service.IsKnownValidator(43))

	service.SetKnownValidators([]uint64{43})

	require.False(t, service.IsKnownValidator(42))
	require.True(t, service.IsKnownValidator(43))
}

func TestService_ResolveValidatorChunkUsesSortedValidatorSet(t *testing.T) {
	service := newTestService(t, commonentities.GatewayTypePartner)
	cfgChunkSize := uint64(64)
	knownValidators := make([]uint64, 0)
	for i := range uint64(68) {
		knownValidators = append(knownValidators, i)
	}
	rand.Shuffle(knownValidators)
	service.SetKnownValidators(knownValidators)

	cases := map[uint64][]uint64{
		10: {0, cfgChunkSize},
		18: {0, cfgChunkSize},
		65: {1, 4},
	}
	for valID, res := range cases {
		chunkID, chunkSize, ok := service.ResolveValidatorChunk(valID)
		require.True(t, ok)
		require.Equal(t, res[0], chunkID)
		require.Equal(t, res[1], chunkSize)
	}

	_, _, ok := service.ResolveValidatorChunk(1999)
	require.False(t, ok)
}

func newTestService(t *testing.T, pairedWith commonentities.GatewayType, validators ...uint64) *message_router.Service {
	t.Helper()

	cnt := test_utils.GetClean(t)
	rig := test_utils.NewAuthTestRig(t, test_utils.WithClaimModifier(func(claims *jwks_verifier.Claims) {
		claims.Type = pairedWith
	}))
	rig.ValidatorIndexes = append([]uint64(nil), validators...)
	m, err := auth_token.New(t.Context(), cnt.Log, rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(cnt.Ctx)
	require.NoError(t, err)

	srv, err := message_router.NewService(t.Context(), &config.AppConfig{
		RemoteBootstrapURL: "dev-bootstrap.getoptimum.io",
	}, cnt.Log, m)
	require.NoError(t, err)
	srv.SetKnownValidators(validators)
	return srv
}

func buildAttestationPayload(t *testing.T, validatorIndex, slot uint64) []byte {
	t.Helper()

	raw, err := hex.DecodeString(validAttestationHex)
	require.NoError(t, err)

	var msg consensus.SingleAttestation
	sszEncoder := &consensus.SSZSnappyCodec{}
	require.NoError(t, sszEncoder.DecodeGossip(raw, &msg))
	require.NotNil(t, msg.Data)

	msg.AttesterIndex = consensus.ValidatorIndex(validatorIndex)
	msg.Data.Slot = consensus.Slot(slot)

	var buf bytes.Buffer
	_, err = sszEncoder.EncodeGossip(&buf, &msg)
	require.NoError(t, err)
	return buf.Bytes()
}
