package gossipsub_gateway

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/aggregator"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const testBlockTopic = "/eth2/deadbeef/beacon_block/ssz_snappy"

func disablePropagation(t *testing.T, svc *Service) {
	t.Helper()
	svc.cfg.PropagationEnabledRaw = false
	svc.cfg.InitDerived()
	require.False(t, svc.cfg.PropagationEnabled())
}

// naiveAggregate wraps one CL attestation in the naive aggregated wire format.
func naiveAggregate(t *testing.T, topic string, attestation []byte) []byte {
	t.Helper()
	payload, err := proto.Marshal(&aggregator.Msg{
		Tms:       time.Now().UnixMilli(),
		Container: []*aggregator.Container{{Topic: topic, Data: [][]byte{attestation}}},
	})
	require.NoError(t, err)
	return append(append([]byte{}, aggregator.PrefixNaive...), payload...)
}

func validAttestation(t *testing.T) []byte {
	t.Helper()
	raw, err := hex.DecodeString(strings.TrimSpace(test_utils.ValidBeaconAttestation31))
	require.NoError(t, err)
	return raw
}

// attestationAt rewrites the fixture so the router's current-slot and known-validator checks pass.
func attestationAt(t *testing.T, attester, slot uint64) []byte {
	t.Helper()
	codec := &consensus.SSZSnappyCodec{}
	var att consensus.SingleAttestation
	require.NoError(t, codec.DecodeGossip(validAttestation(t), &att))
	att.AttesterIndex = consensus.ValidatorIndex(attester)
	att.Data.Slot = consensus.Slot(slot)
	var buf bytes.Buffer
	_, err := codec.EncodeGossip(&buf, &att)
	require.NoError(t, err)
	return buf.Bytes()
}

func attestationsForwardedToMumP2P(t *testing.T) float64 {
	t.Helper()
	families, err := telemetry.CustomRegistry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if strings.HasSuffix(family.GetName(), "attestation_forwarded_mump2p_total") {
			require.NotEmpty(t, family.Metric)
			return family.Metric[0].GetCounter().GetValue()
		}
	}
	t.Fatal("attestation_forwarded_mump2p_total is not registered")
	return 0
}

// The switch only gates mump2p -> CL: CL blocks and attestations still go out to mump2p.
func TestPropagationDisabledKeepsCLToMumP2P(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)
	disablePropagation(t, svc)

	// dedup runs after the arrival gate, so a marked hash means the block reached the publish step
	block := blockAtSlot(t, test_utils.HoodiBeaconBlockMessage1, chainstate.CurrentSlot(time.Now()))
	svc.processCLBeaconBlock(svc.log, &entities.CLMessage{Topic: testBlockTopic, Message: block})
	require.True(t, svc.isDuplicateMessage(block), "CL block must still reach the mump2p publish path")

	const attester = 42
	svc.srvMsgRouter.SetKnownValidators([]uint64{attester})
	attestationTopic := "/eth2/deadbeef/beacon_attestation_31/ssz_snappy"
	attestation := attestationAt(t, attester, chainstate.CurrentSlot(time.Now()))
	before := attestationsForwardedToMumP2P(t)
	svc.processCLAttestation(svc.log, &entities.CLMessage{Topic: attestationTopic, Message: attestation}, topics.TopicMetaFor(attestationTopic))
	require.Equal(t, before+1, attestationsForwardedToMumP2P(t), "CL attestation must still be forwarded to mump2p")
}

// With propagation off, neither blocks nor aggregated attestations from mump2p reach the CL.
func TestPropagationSwitchMumP2PToCL(t *testing.T) {
	attestationTopic := "/eth2/deadbeef/beacon_attestation_31/ssz_snappy"

	for _, tc := range []struct {
		name        string
		propagation bool
	}{
		{"enabled forwards both", true},
		{"disabled forwards nothing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newGateway(t)
			t.Cleanup(svc.messagesMap.Close)
			if !tc.propagation {
				disablePropagation(t, svc)
			}
			blockSub := joinCLTopic(t, svc, testBlockTopic)
			attestationSub := joinCLTopic(t, svc, attestationTopic)

			svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
				SourceNodeID: testPeerID, Topic: testBlockTopic,
				Message: blockAtSlot(t, test_utils.HoodiBeaconBlockMessage1, chainstate.CurrentSlot(time.Now())),
			})
			svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
				SourceNodeID: testPeerID, Topic: mumP2PAggregatedMessagesTopic,
				Message: naiveAggregate(t, attestationTopic, validAttestation(t)),
			})

			wait := 5 * time.Second
			if !tc.propagation {
				wait = 250 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), wait)
			defer cancel()
			_, blockErr := blockSub.Next(ctx)
			_, attestationErr := attestationSub.Next(ctx)
			if tc.propagation {
				require.NoError(t, blockErr, "block must reach the CL")
				require.NoError(t, attestationErr, "attestation must reach the CL")
				return
			}
			require.ErrorIs(t, blockErr, context.DeadlineExceeded, "block must not reach the CL")
			require.ErrorIs(t, attestationErr, context.DeadlineExceeded, "attestation must not reach the CL")
		})
	}
}
