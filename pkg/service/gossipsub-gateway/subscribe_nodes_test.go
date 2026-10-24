package gossipsub_gateway

import (
	"testing"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
)

func TestFilterEthTopics_MumP2PNode(t *testing.T) {
	// given
	beaconBlockTopic := "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	svc, _ := newGateway(t)

	t.Run("should be 2 topics for mump2p", func(t *testing.T) {
		// when
		got := svc.filterAndBuildEthTopics("c6ecb76c", true)

		// then
		require.Contains(t, got, beaconBlockTopic)
		require.Contains(t, got, mumP2PAggregatedMessagesTopic)
		require.Len(t, got, 2)
	})
	t.Run("should be 65 topics for cl", func(t *testing.T) {
		// when
		got := svc.filterAndBuildEthTopics("c6ecb76c", false)

		// then
		require.Contains(t, got, beaconBlockTopic)
		require.Len(t, got, 65)
		for _, topic := range got {
			require.Contains(t, topic, "c6ecb76c")
		}
	})
}

func TestUnsubscribeHandlerTopicPreservesReplacement(t *testing.T) {
	svc, _ := newGateway(t)
	topic := "/eth2/deadbeef/beacon_attestation_5/ssz_snappy"
	stale := &libp2ppubsub.Subscription{}
	replacement := &libp2ppubsub.Subscription{}
	svc.libP2PSubs.Store(topic, replacement)

	svc.unsubscribeHandlerTopic(topic, stale)

	current, ok := svc.libP2PSubs.Load(topic)
	require.True(t, ok)
	require.Same(t, replacement, current)
}
