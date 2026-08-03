package gossipsub_gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
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
	t.Run("the declared publish topics cover what is joined", func(t *testing.T) {
		// The coded-symbol size reserves room for the longest topic, and the
		// reservation is made from the declared set before any topic is joined.
		// A topic joined here that is longer than anything declared puts every
		// symbol on it past the datagram budget and onto the stream fallback,
		// which nothing else in this suite would notice.
		got := svc.filterAndBuildEthTopics("c6ecb76c", true)

		declared := 0
		for _, topic := range topics.MumP2PPublishTopics() {
			declared = max(declared, len(topic))
		}

		for _, topic := range got {
			require.LessOrEqualf(t, len(topic), declared,
				"joined topic %q is %d bytes, longer than the %d bytes declared to the protocol",
				topic, len(topic), declared)
		}
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
