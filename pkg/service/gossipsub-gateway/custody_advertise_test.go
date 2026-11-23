package gossipsub_gateway

import (
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func startReqRespGateway(t *testing.T) (*Service, host.Host) {
	t.Helper()
	svc, _ := newGateway(t)
	startForkGatewayHost(t, svc)
	svc.handleHandshake()

	client, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Connect(t.Context(), peer.AddrInfo{ID: svc.hostLibP2P.ID(), Addrs: svc.hostLibP2P.Addrs()}))
	return svc, client
}

func request(t *testing.T, svc *Service, client host.Host, proto protocol.ID, req consensus.Marshaler, resp consensus.Unmarshaler) {
	t.Helper()
	stream, err := client.NewStream(t.Context(), svc.hostLibP2P.ID(), proto)
	require.NoError(t, err)
	defer stream.Close()
	require.NoError(t, stream.SetDeadline(time.Now().Add(5*time.Second)))
	if req != nil {
		_, err = svc.sszEncoder.EncodeWithMaxLength(stream, req)
		require.NoError(t, err)
	}
	require.NoError(t, stream.CloseWrite())

	code := make([]byte, 1)
	_, err = stream.Read(code)
	require.NoError(t, err)
	require.Equal(t, entities.ResponseCodeSuccess, code[0])
	require.NoError(t, svc.sszEncoder.DecodeWithMaxLength(stream, resp))
}

func supportedStatusV2(t *testing.T) *consensus.StatusV2 {
	t.Helper()
	digest, err := hex.DecodeString(forkCurDigest)
	require.NoError(t, err)
	st := &consensus.StatusV2{
		FinalizedRoot:         test_utils.Arr32(1),
		FinalizedEpoch:        10,
		HeadRoot:              test_utils.Arr32(2),
		HeadSlot:              400,
		EarliestAvailableSlot: 32,
	}
	copy(st.ForkDigest[:], digest)
	return st
}

func TestStatusV2ReplyAdvertisesNoData(t *testing.T) {
	svc, client := startReqRespGateway(t)
	req := supportedStatusV2(t)

	got := new(consensus.StatusV2)
	request(t, svc, client, statusV2Protocol, req, got)

	want := *req
	want.EarliestAvailableSlot = noDataEarliestAvailableSlot
	require.Equal(t, want, *got)
}

func TestAdvertisedStatusCopiesInput(t *testing.T) {
	in := supportedStatusV2(t)
	out := advertisedStatus(in)
	require.Equal(t, noDataEarliestAvailableSlot, out.(*consensus.StatusV2).EarliestAvailableSlot)
	require.EqualValues(t, 32, in.EarliestAvailableSlot, "the stored peer status must not change")
}

func TestStatusV1ReplyIsEcho(t *testing.T) {
	svc, client := startReqRespGateway(t)
	v2 := supportedStatusV2(t)
	req := &consensus.Status{
		ForkDigest:     v2.ForkDigest,
		FinalizedRoot:  v2.FinalizedRoot,
		FinalizedEpoch: v2.FinalizedEpoch,
		HeadRoot:       v2.HeadRoot,
		HeadSlot:       v2.HeadSlot,
	}

	got := new(consensus.Status)
	request(t, svc, client, statusV1Protocol, req, got)
	require.Equal(t, *req, *got)
}

func TestMetadataV3AdvertisesMinimumCustody(t *testing.T) {
	svc, client := startReqRespGateway(t)

	got := new(consensus.MetaDataV2)
	request(t, svc, client, "/eth2/beacon_chain/req/metadata/3/ssz_snappy", nil, got)
	require.EqualValues(t, 4, got.CustodyGroupCount)
}

func statusPeer(t *testing.T, svc *Service, protos ...protocol.ID) (cl host.Host, got <-chan consensus.StatusMessage) {
	t.Helper()
	cl, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	sent := make(chan consensus.StatusMessage, 1)
	for _, proto := range protos {
		cl.SetStreamHandler(proto, func(stream network.Stream) {
			defer stream.Close()
			st := newStatusMessage(stream.Protocol())
			if err := svc.sszEncoder.DecodeWithMaxLength(stream, st); err != nil {
				return
			}
			// Only accept a request the gateway half-closed, as the spec requires.
			_ = stream.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := stream.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				return
			}
			sent <- st
			_, _ = stream.Write([]byte{entities.ResponseCodeSuccess})
			_, _ = svc.sszEncoder.EncodeWithMaxLength(stream, st)
		})
	}
	require.NoError(t, svc.hostLibP2P.Connect(t.Context(), peer.AddrInfo{ID: cl.ID(), Addrs: cl.Addrs()}))
	return cl, sent
}

func receivedStatus(t *testing.T, got <-chan consensus.StatusMessage) consensus.StatusMessage {
	t.Helper()
	select {
	case st := <-got:
		return st
	case <-time.After(5 * time.Second):
		t.Fatal("no status handshake reached the dialed peer")
		return nil
	}
}

func TestOutboundHandshakeAdvertisesNoData(t *testing.T) {
	svc, client := startReqRespGateway(t)
	request(t, svc, client, statusV2Protocol, supportedStatusV2(t), new(consensus.StatusV2))
	cl, got := statusPeer(t, svc, statusV2Protocol, statusV1Protocol)

	require.NoError(t, svc.sendHandshakeForPeer(t.Context(), cl.ID()))
	sent := receivedStatus(t, got).(*consensus.StatusV2)
	require.Equal(t, noDataEarliestAvailableSlot, sent.EarliestAvailableSlot)
	require.EqualValues(t, 400, sent.HeadSlot)
}

func TestOutboundHandshakeAfterStatusV1StillUsesV2(t *testing.T) {
	svc, client := startReqRespGateway(t)
	v2 := supportedStatusV2(t)
	request(t, svc, client, statusV2Protocol, v2, new(consensus.StatusV2))
	v1 := &consensus.Status{ForkDigest: v2.ForkDigest, FinalizedEpoch: 11, HeadSlot: 401}
	request(t, svc, client, statusV1Protocol, v1, new(consensus.Status))
	cl, got := statusPeer(t, svc, statusV2Protocol, statusV1Protocol)

	require.NoError(t, svc.sendHandshakeForPeer(t.Context(), cl.ID()))
	sent := receivedStatus(t, got).(*consensus.StatusV2)
	require.Equal(t, noDataEarliestAvailableSlot, sent.EarliestAvailableSlot)
	require.EqualValues(t, 401, sent.HeadSlot, "chain fields come from the latest status, whatever its version")
}

func TestOutboundHandshakeFallsBackToStatusV1(t *testing.T) {
	svc, client := startReqRespGateway(t)
	request(t, svc, client, statusV2Protocol, supportedStatusV2(t), new(consensus.StatusV2))
	cl, got := statusPeer(t, svc, statusV1Protocol)

	require.NoError(t, svc.sendHandshakeForPeer(t.Context(), cl.ID()))
	sent := receivedStatus(t, got)
	require.IsType(t, &consensus.Status{}, sent)
	require.EqualValues(t, 400, sent.(*consensus.Status).HeadSlot)
}
