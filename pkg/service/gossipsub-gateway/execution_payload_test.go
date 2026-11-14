package gossipsub_gateway

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const testExecutionPayloadTopic = "/eth2/deadbeef/execution_payload/ssz_snappy"

func TestExecutionPayloadLimitStaysInsideTheMeshCap(t *testing.T) {
	mesh, err := utils.CalculateMaxSize(config.DefaultMaxMessageSize)
	require.NoError(t, err)
	require.Equal(t, mesh, executionPayloadLimit(true))
	require.Less(t, executionPayloadLimit(true), consensus.MaxCompressedGossipSize())
	require.Equal(t, consensus.MaxCompressedGossipSize(), executionPayloadLimit(false))
}

func TestExecutionPayloadOversizeIsNotPublished(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)

	mesh, err := utils.CalculateMaxSize(config.DefaultMaxMessageSize)
	require.NoError(t, err)
	tooBigForMesh := make([]byte, mesh+1)
	svc.processCLExecutionPayload(svc.log, &entities.CLMessage{
		Topic: testExecutionPayloadTopic, Message: tooBigForMesh,
	})
	_, seen := svc.messagesMap.Get(commonhash.XXHash(tooBigForMesh))
	require.False(t, seen, "an oversized payload must be skipped before the mesh publish")
	require.Equal(t, float64(1), executionPayloadSkips(t, "mump2p"))

	small := []byte{1, 2, 3}
	svc.processCLExecutionPayload(svc.log, &entities.CLMessage{
		Topic: testExecutionPayloadTopic, Message: small,
	})
	_, seen = svc.messagesMap.Get(commonhash.XXHash(small))
	require.True(t, seen, "a payload inside the mesh budget reaches the publish path")
	require.Equal(t, float64(1), executionPayloadCount(t, "execution_payload_received_total", "cl"))

	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: testExecutionPayloadTopic, Message: []byte{4, 5, 6},
	})
	require.Equal(t, float64(1), executionPayloadCount(t, "execution_payload_received_total", "mump2p"))

	tooBigForCL := make([]byte, consensus.MaxCompressedGossipSize()+1)
	sub := joinCLTopic(t, svc, testExecutionPayloadTopic)
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: testExecutionPayloadTopic, Message: tooBigForCL,
	})
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err = sub.Next(ctx)
	require.Error(t, err, "an oversized payload must not reach the CL")
	require.Equal(t, float64(1), executionPayloadSkips(t, "cl"))
}

func executionPayloadSkips(t *testing.T, direction string) float64 {
	t.Helper()
	return executionPayloadCount(t, "execution_payload_skipped_total", direction)
}

func executionPayloadCount(t *testing.T, name, direction string) float64 {
	t.Helper()
	families, err := telemetry.CustomRegistry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if !strings.HasSuffix(family.GetName(), name) {
			continue
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				if label.GetName() == "direction" && label.GetValue() == direction {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	t.Fatalf("%s direction=%s is not registered", name, direction)
	return 0
}
