package consensus_test

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestMsgID(t *testing.T) {
	precalculatedMessagesMap := map[string]string{
		test_utils.ETHTestTopicBlock:       "90fd60a5447ec5a0cfd87c1c5e06ecc469061007",
		test_utils.ETHTestTopicAttestation: "6c4b48480d1d41a2eced6d337b9e111d9b1e3486",
		test_utils.ETHTestTopicAggregate:   "1bddf4aff70c10f271a13957a10f9d6e252f9623",
	}
	for topic, payload := range test_utils.TopicMessages {
		data, err := hex.DecodeString(payload)
		require.NoError(t, err)
		hexID := hex.EncodeToString([]byte(consensus.MsgID(topic, data)))
		require.Equal(t, precalculatedMessagesMap[topic], hexID)
	}
}
