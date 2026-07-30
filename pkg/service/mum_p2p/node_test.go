package mum_p2p_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	commontest "github.com/getoptimum/optimum-common/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestNewNodeStartsWithQUIC(t *testing.T) {
	cnt := test_utils.GetClean(t)
	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, "optimum_quic_smoke", commontest.GetFreePortT(t), nil)
	node, err := mum_p2p.NewNode(cnt.Ctx, cnt.Log, cfg, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(node.Stop)
}

func TestNodePublishMessageAutoSubscribes(t *testing.T) {
	cnt := test_utils.GetClean(t)
	node := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_test_auto_subscribe", t.TempDir())

	require.NoError(t, node.PublishMessage(cnt.Ctx, "auto-topic", []byte("payload")))
	require.Contains(t, node.GetTopics(), "auto-topic")

	totalPeers, perTopicPeers := node.CountConnectedPeers()
	require.Zero(t, totalPeers)
	require.Zero(t, perTopicPeers["auto-topic"])
}

func TestNodeHandshakeAndTopicLifecycle(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_test_topic_flow"

	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())

	test_utils.ConnectNodes(cnt.Ctx, t, nodeA, nodeB)

	const topic = "beacon_attestation_31"
	require.NoError(t, nodeA.SubscribeTopic(topic))
	require.NoError(t, nodeA.SubscribeTopic(topic))
	require.NoError(t, nodeB.SubscribeTopic(topic))

	require.Eventually(t, func() bool {
		_, perTopicPeers := nodeA.CountConnectedPeers()
		return perTopicPeers[topic] > 0
	}, 10*time.Second, 100*time.Millisecond)

	listener := nodeB.RegisterListener("topic-flow")
	t.Cleanup(func() {
		nodeB.UnregisterListener("topic-flow")
	})

	payload := []byte("hello from mump2p")
	require.NoError(t, nodeA.PublishMessage(cnt.Ctx, topic, payload))

	select {
	case msg := <-listener:
		require.Equal(t, entities.MumP2PCommandMessage, msg.Command)
		require.Equal(t, topic, msg.Message.Topic)
		require.Equal(t, payload, msg.Message.Message)
		require.Equal(t, nodeB.GetHostInfo().ID.String(), msg.Message.UpstreamPeerID)
		require.Equal(t, nodeA.GetHostInfo().ID.String(), msg.Message.SourceNodeID)
		require.NotEmpty(t, msg.Message.MessageID)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for forwarded topic message")
	}

	require.Len(t, nodeA.GetPeers(), 1)
	require.Len(t, nodeB.GetPeers(), 1)

	require.NoError(t, nodeB.UnsubscribeTopic(topic))
	require.NotContains(t, nodeB.GetTopics(), topic)
	require.NoError(t, nodeB.UnsubscribeTopic(topic))
}

func TestNodeDisconnectsPeerOnClusterMismatch(t *testing.T) {
	cnt := test_utils.GetClean(t)

	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_cluster_a", t.TempDir())
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_cluster_b", t.TempDir())

	require.NoError(t, nodeA.GetHost().Connect(cnt.Ctx, nodeB.GetHostInfo()))

	require.Eventually(t, func() bool {
		return len(nodeA.GetPeers()) == 0 && len(nodeB.GetPeers()) == 0
	}, 10*time.Second, 100*time.Millisecond)
}

func TestNodeRestoresPersistedTopicsOnRestart(t *testing.T) {
	cnt := test_utils.GetClean(t)
	identityDir := t.TempDir()

	node := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_test_persisted_topics", identityDir)
	require.NoError(t, node.SubscribeTopic("persisted-topic"))
	require.Contains(t, node.GetTopics(), "persisted-topic")
	require.Eventually(t, func() bool {
		_, err := os.Stat(filepath.Join(identityDir, "topics.dump"))
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)

	restarted := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, "optimum_test_persisted_topics", identityDir)
	require.Contains(t, restarted.GetTopics(), "persisted-topic")
}

func TestNodeUsesCustomHandshakeHooks(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_custom_handshake"

	var handledA atomic.Int32
	var handledB atomic.Int32

	nodeA := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir(), customHandshakeOptions(&handledA)...)
	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir(), customHandshakeOptions(&handledB)...)

	test_utils.ConnectNodes(cnt.Ctx, t, nodeA, nodeB)

	require.Eventually(t, func() bool {
		return handledA.Load() > 0 && handledB.Load() > 0
	}, 10*time.Second, 100*time.Millisecond)
}

func customHandshakeOptions(counter *atomic.Int32) []mum_p2p.NodeOption {
	type customHandshake struct {
		Kind string `json:"kind"`
	}

	return []mum_p2p.NodeOption{
		mum_p2p.WithCustomHandshakeBuilder(func() any {
			return customHandshake{Kind: "custom"}
		}),
		mum_p2p.WithCustomHandshakeHandler(func(_ peer.ID, decoder *json.Decoder) error {
			var handshake customHandshake
			if err := decoder.Decode(&handshake); err != nil {
				return err
			}
			if handshake.Kind != "custom" {
				return fmt.Errorf("unexpected handshake kind %q", handshake.Kind)
			}
			counter.Add(1)
			return nil
		}),
	}
}
