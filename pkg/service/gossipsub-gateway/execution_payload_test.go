package gossipsub_gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const testExecutionPayloadTopic = "/eth2/deadbeef/execution_payload/ssz_snappy"

func TestExecutionPayloadAboveTheOldMeshCapIsPublished(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)

	mesh, err := utils.CalculateMaxSize(config.DefaultMaxMessageSize)
	require.NoError(t, err)
	payload := make([]byte, mesh+1)
	beforeRecv := executionPayloadCount(t, "execution_payload_received_total", "cl")
	beforeSkip := executionPayloadCount(t, "execution_payload_skipped_total", "mump2p")

	svc.processCLExecutionPayload(svc.log, &entities.CLMessage{
		Topic: testExecutionPayloadTopic, Message: payload,
	})

	_, seen := svc.messagesMap.Get(commonhash.XXHash(payload))
	require.True(t, seen, "a payload above the old 1 MiB mesh budget still reaches the publish path")
	require.Equal(t, beforeRecv+1, executionPayloadCount(t, "execution_payload_received_total", "cl"))
	require.Equal(t, beforeSkip, executionPayloadCount(t, "execution_payload_skipped_total", "mump2p"))
}

func TestExecutionPayloadBoundary(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)

	exact := make([]byte, consensus.MaxCompressedGossipSize())
	exact[0] = 1
	beforeRecv := executionPayloadCount(t, "execution_payload_received_total", "cl")
	beforeSkip := executionPayloadCount(t, "execution_payload_skipped_total", "mump2p")
	svc.processCLExecutionPayload(svc.log, &entities.CLMessage{
		Topic: testExecutionPayloadTopic, Message: exact,
	})
	_, seen := svc.messagesMap.Get(commonhash.XXHash(exact))
	require.True(t, seen, "a payload at the consensus budget must be forwarded")
	require.Equal(t, beforeRecv+1, executionPayloadCount(t, "execution_payload_received_total", "cl"))
	require.Equal(t, beforeSkip, executionPayloadCount(t, "execution_payload_skipped_total", "mump2p"))

	over := make([]byte, consensus.MaxCompressedGossipSize()+1)
	over[0] = 2
	svc.processCLExecutionPayload(svc.log, &entities.CLMessage{
		Topic: testExecutionPayloadTopic, Message: over,
	})
	_, seen = svc.messagesMap.Get(commonhash.XXHash(over))
	require.False(t, seen, "one byte over the consensus budget must be skipped before publish")
	require.Equal(t, beforeSkip+1, executionPayloadCount(t, "execution_payload_skipped_total", "mump2p"))
}

func TestExecutionPayloadTracerLogsEveryLibP2PCopy(t *testing.T) {
	logs, buf := arrivalLogger()
	tr := &executionPayloadTracer{log: logs}
	payload := []byte{9, 8, 7}
	from := peer.ID("prysm")

	tr.ValidateMessage(pubsubMessage(testExecutionPayloadTopic, payload, from))
	tr.DuplicateMessage(pubsubMessage(testExecutionPayloadTopic, payload, from))
	tr.ValidateMessage(pubsubMessage("/eth2/deadbeef/beacon_block/ssz_snappy", payload, from))
	tr.ValidateMessage(pubsubMessage("/eth2/deadbeef/execution_payload_bid/ssz_snappy", payload, from))

	lines := arrivals(t, buf)
	require.Len(t, lines, 2, "first and duplicate copies are logged, other topics are not")
	for i, line := range lines {
		require.Equal(t, "cl", line["direction"])
		require.Equal(t, executionPayloadHash(payload), line["payload_hash"])
		require.Equal(t, from.String(), line["peer"])
		require.Equal(t, i == 0, line["first_seen"])
		require.Positive(t, line["received_at_ms"])
	}
}

func TestExecutionPayloadMumP2PArrivalIsStampedAtReceive(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	logs, buf := arrivalLogger()
	svc.log = logs
	payload := []byte{9, 8, 7}

	svc.logMumP2PExecutionPayloadArrival(&commonentities.P2PMessage{
		SourceNodeID: svc.nodeMumP2PStr, Topic: testExecutionPayloadTopic, Message: payload,
	})
	svc.logMumP2PExecutionPayloadArrival(&commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: "/eth2/deadbeef/beacon_block/ssz_snappy", Message: payload,
	})
	svc.logMumP2PExecutionPayloadArrival(&commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: testExecutionPayloadTopic, Message: make([]byte, consensus.MaxCompressedGossipSize()+1),
	})
	svc.logMumP2PExecutionPayloadArrival(&commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: testExecutionPayloadTopic, Message: payload,
	})

	lines := arrivals(t, buf)
	require.Len(t, lines, 1, "only an in-budget envelope from another node is logged")
	require.Equal(t, "mump2p", lines[0]["direction"])
	require.Equal(t, testPeerID, lines[0]["source_node_id"])
	require.Equal(t, executionPayloadHash(payload), lines[0]["payload_hash"], "joins the libp2p line for the same envelope")
}

func arrivalLogger() (logger.AppLogger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return logger.InitLogger([]io.Writer{buf}, logger.Info), buf
}

func arrivals(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		line := map[string]any{}
		require.NoError(t, json.Unmarshal(raw, &line))
		if line["msg"] == "execution_payload arrived" {
			out = append(out, line)
		}
	}
	return out
}

func pubsubMessage(topic string, data []byte, from peer.ID) *libp2ppubsub.Message {
	return &libp2ppubsub.Message{
		Message:      &pubsubpb.Message{Topic: &topic, Data: data},
		ReceivedFrom: from,
	}
}

func TestExecutionPayloadDuplicateCountsOnce(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)

	payload := []byte{1, 2, 3}
	before := executionPayloadCount(t, "execution_payload_received_total", "cl")
	msg := &entities.CLMessage{Topic: testExecutionPayloadTopic, Message: payload}
	svc.processCLExecutionPayload(svc.log, msg)
	svc.processCLExecutionPayload(svc.log, msg)
	require.Equal(t, before+1, executionPayloadCount(t, "execution_payload_received_total", "cl"))
}

func TestExecutionPayloadReachesTheCL(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)
	sub := joinCLTopic(t, svc, testExecutionPayloadTopic)

	payload := []byte{7, 8, 9}
	before := executionPayloadCount(t, "execution_payload_received_total", "mump2p")
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: testExecutionPayloadTopic, Message: payload,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	got, err := sub.Next(ctx)
	require.NoError(t, err, "an in-budget envelope must reach the CL")
	require.Equal(t, payload, got.Data)
	require.Equal(t, before+1, executionPayloadCount(t, "execution_payload_received_total", "mump2p"))

	beforeSkip := executionPayloadCount(t, "execution_payload_skipped_total", "cl")
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: testPeerID,
		Topic:        testExecutionPayloadTopic,
		Message:      make([]byte, consensus.MaxCompressedGossipSize()+1),
	})
	idle, cancelIdle := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancelIdle()
	_, err = sub.Next(idle)
	require.Error(t, err, "an oversized envelope must not reach the CL")
	require.Equal(t, beforeSkip+1, executionPayloadCount(t, "execution_payload_skipped_total", "cl"))
}

func TestExecutionPayloadRelayDoesNotReachTheCL(t *testing.T) {
	svc, _ := newGatewayOfType(t, commonentities.GatewayTypeRelay)
	t.Cleanup(svc.messagesMap.Close)
	sub := joinCLTopic(t, svc, testExecutionPayloadTopic)

	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: testPeerID, Topic: testExecutionPayloadTopic, Message: []byte{4, 5, 6},
	})

	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	_, err := sub.Next(ctx)
	require.Error(t, err, "a relay must not deliver the envelope to its CL")
}

func TestExecutionPayloadPropagationOffDoesNotCountSkip(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)
	disablePropagation(t, svc)

	before := executionPayloadCount(t, "execution_payload_skipped_total", "cl")
	svc.processMumP2PMessage(svc.log, &commonentities.P2PMessage{
		SourceNodeID: testPeerID,
		Topic:        testExecutionPayloadTopic,
		Message:      make([]byte, consensus.MaxCompressedGossipSize()+1),
	})
	require.Equal(t, before, executionPayloadCount(t, "execution_payload_skipped_total", "cl"))
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
	return 0
}
