package consensus_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestCodecGossipRoundTrip(t *testing.T) {
	var codec consensus.SSZSnappyCodec

	msg := &consensus.SingleAttestation{
		CommitteeIndex: 7, AttesterIndex: 42,
		Data: consensus.AttestationData{
			Slot: 111, CommitteeIndex: 3, BeaconBlockRoot: test_utils.Arr32(0xaa),
			Source: consensus.Checkpoint{Epoch: 5, Root: test_utils.Arr32(0xbb)},
			Target: consensus.Checkpoint{Epoch: 6, Root: test_utils.Arr32(0xcc)},
		},
		Signature: test_utils.Arr96(0xdd),
	}

	var buf bytes.Buffer
	_, err := codec.EncodeGossip(&buf, msg)
	require.NoError(t, err)
	require.NotEmpty(t, buf.Bytes())

	var got consensus.SingleAttestation
	require.NoError(t, codec.DecodeGossip(buf.Bytes(), &got))
	require.Equal(t, *msg, got)

	raw, err := codec.DecodeGossipRaw(buf.Bytes())
	require.NoError(t, err)
	wantRaw, err := msg.MarshalSSZ()
	require.NoError(t, err)
	require.Equal(t, wantRaw, raw)
}

func TestCodecReqRespRoundTrip(t *testing.T) {
	var codec consensus.SSZSnappyCodec

	msg := &consensus.Status{
		ForkDigest:    [4]byte{1, 2, 3, 4},
		FinalizedRoot: test_utils.Arr32(0xaa), FinalizedEpoch: 5,
		HeadRoot: test_utils.Arr32(0xbb), HeadSlot: 99,
	}

	var buf bytes.Buffer
	_, err := codec.EncodeWithMaxLength(&buf, msg)
	require.NoError(t, err)
	require.NotEmpty(t, buf.Bytes())

	var got consensus.Status
	require.NoError(t, codec.DecodeWithMaxLength(bytes.NewReader(buf.Bytes()), &got))
	require.Equal(t, *msg, got)
}

type rawMsg []byte

func (r rawMsg) MarshalSSZ() ([]byte, error)  { return []byte(r), nil }
func (r *rawMsg) UnmarshalSSZ(b []byte) error { *r = append((*r)[:0], b...); return nil }

func TestCodecReqRespLargePayloads(t *testing.T) {
	var codec consensus.SSZSnappyCodec

	large := make([]byte, 200*1024)
	for i := range large {
		large[i] = byte(i * 7)
	}
	for _, payload := range []rawMsg{{}, rawMsg(large)} {
		var buf bytes.Buffer
		_, err := codec.EncodeWithMaxLength(&buf, payload)
		require.NoError(t, err)

		var got rawMsg
		require.NoError(t, codec.DecodeWithMaxLength(bytes.NewReader(buf.Bytes()), &got))
		require.True(t, bytes.Equal(payload, got))
	}
}

func TestCodecRejectsOversized(t *testing.T) {
	var codec consensus.SSZSnappyCodec
	var got rawMsg

	oversize := binary.AppendUvarint(nil, utils.MaxGossipPayloadSize+1)
	require.Error(t, codec.DecodeWithMaxLength(bytes.NewReader(oversize), &got))

	require.Error(t, codec.DecodeGossip(make([]byte, 13*1024*1024), &got))
}
