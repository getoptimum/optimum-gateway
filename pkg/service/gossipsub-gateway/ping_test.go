package gossipsub_gateway

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
)

func TestPingRepliesWithOwnMetadataSeq(t *testing.T) {
	svc, client := startReqRespGateway(t)

	peerSeq := consensus.SSZUint64(57)
	pong := new(consensus.SSZUint64)
	request(t, svc, client, "/eth2/beacon_chain/req/ping/1/ssz_snappy", &peerSeq, pong)
	require.EqualValues(t, metadataSeqNumber, *pong)

	v0 := new(consensus.MetaDataV0)
	request(t, svc, client, "/eth2/beacon_chain/req/metadata/1/ssz_snappy", nil, v0)
	v1 := new(consensus.MetaDataV1)
	request(t, svc, client, "/eth2/beacon_chain/req/metadata/2/ssz_snappy", nil, v1)
	v2 := new(consensus.MetaDataV2)
	request(t, svc, client, "/eth2/beacon_chain/req/metadata/3/ssz_snappy", nil, v2)
	for _, seq := range []uint64{v0.SeqNumber, v1.SeqNumber, v2.SeqNumber} {
		require.EqualValues(t, *pong, seq, "metadata seq must match the ping reply")
	}
}
