package gossipsub_gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
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

func TestMalformedPingGetsInvalidRequest(t *testing.T) {
	for name, payload := range map[string][]byte{
		"corrupt frame": {0x03, 0xff}, // length 3, then a truncated snappy frame
		"empty request": nil,          // half-closed before writing anything
	} {
		t.Run(name, func(t *testing.T) { requireInvalidPing(t, payload) })
	}
}

func requireInvalidPing(t *testing.T, payload []byte) {
	t.Helper()
	svc, client := startReqRespGateway(t)
	stream, err := client.NewStream(t.Context(), svc.hostLibP2P.ID(), protocol.ID("/eth2/beacon_chain/req/ping/1/ssz_snappy"))
	require.NoError(t, err)
	defer stream.Close()
	require.NoError(t, stream.SetDeadline(time.Now().Add(5*time.Second)))
	if payload != nil {
		_, err = stream.Write(payload)
		require.NoError(t, err)
	}
	require.NoError(t, stream.CloseWrite())

	code := make([]byte, 1)
	_, err = stream.Read(code)
	require.NoError(t, err)
	require.Equal(t, entities.ResponseCodeInvalidRequest, code[0])
	var msg consensus.ErrorMessage
	require.NoError(t, svc.sszEncoder.DecodeWithMaxLength(stream, &msg), "InvalidRequest must carry an ErrorMessage")
	require.Equal(t, consensus.ErrorMessage("invalid ping request"), msg)
}

// metadataHistory is append-only: entry i is the content hash advertised at seq i+1.
var metadataHistory = []string{
	"d5252e125f558674e47b797918c098780461a271ea1613f27e59c36c211dac0f",
}

// Fails when the advertised metadata changes without a metadataSeqNumber bump.
func TestMetadataSeqHistory(t *testing.T) {
	h := sha256.New()
	for _, proto := range []string{"metadata/1", "metadata/2", "metadata/3"} {
		msg := newMetadataMessage(proto)
		require.NotNil(t, msg, proto)
		switch m := msg.(type) { // hash content only, so bumping the seq alone changes nothing
		case *consensus.MetaDataV0:
			m.SeqNumber = 0
		case *consensus.MetaDataV1:
			m.SeqNumber = 0
		case *consensus.MetaDataV2:
			m.SeqNumber = 0
		}
		b, err := msg.MarshalSSZ()
		require.NoError(t, err)
		h.Write(b)
	}
	require.Len(t, metadataHistory, metadataSeqNumber, "append the new hash and bump metadataSeqNumber together")
	require.Equal(t, metadataHistory[metadataSeqNumber-1], hex.EncodeToString(h.Sum(nil)),
		"metadata changed: bump metadataSeqNumber and append the new hash")
}
