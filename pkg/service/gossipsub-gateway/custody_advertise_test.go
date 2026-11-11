package gossipsub_gateway

import (
	"encoding/hex"
	"testing"

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

func request(t *testing.T, svc *Service, client host.Host, proto string, req consensus.Marshaler, resp consensus.Unmarshaler) {
	t.Helper()
	stream, err := client.NewStream(t.Context(), svc.hostLibP2P.ID(), protocol.ID(proto))
	require.NoError(t, err)
	defer stream.Close()
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
	request(t, svc, client, "/eth2/beacon_chain/req/status/2/ssz_snappy", req, got)

	want := *req
	want.EarliestAvailableSlot = noDataEarliestAvailableSlot
	require.Equal(t, want, *got)

	stored := svc.networkStatus.Load()
	require.NotNil(t, stored)
	require.Equal(t, req, stored.status, "stored status must stay the peer's own")
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
	request(t, svc, client, "/eth2/beacon_chain/req/status/1/ssz_snappy", req, got)
	require.Equal(t, *req, *got)
}

func TestMetadataV3AdvertisesMinimumCustody(t *testing.T) {
	svc, client := startReqRespGateway(t)

	got := new(consensus.MetaDataV2)
	request(t, svc, client, "/eth2/beacon_chain/req/metadata/3/ssz_snappy", nil, got)
	require.EqualValues(t, 4, got.CustodyGroupCount)
}

func TestOutboundHandshakeAdvertisesNoData(t *testing.T) {
	svc, _ := newGateway(t)
	startForkGatewayHost(t, svc)
	stored := supportedStatusV2(t)
	svc.setNetworkBeaconStatus("/eth2/beacon_chain/req/status/2/ssz_snappy", stored)

	cl, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = cl.Close() })
	got := make(chan *consensus.StatusV2, 1)
	cl.SetStreamHandler("/eth2/beacon_chain/req/status/2/ssz_snappy", func(stream network.Stream) {
		defer stream.Close()
		st := new(consensus.StatusV2)
		if err := svc.sszEncoder.DecodeWithMaxLength(stream, st); err != nil {
			return
		}
		got <- st
		_, _ = stream.Write([]byte{entities.ResponseCodeSuccess})
		_, _ = svc.sszEncoder.EncodeWithMaxLength(stream, st)
	})
	require.NoError(t, svc.hostLibP2P.Connect(t.Context(), peer.AddrInfo{ID: cl.ID(), Addrs: cl.Addrs()}))

	require.NoError(t, svc.sendHandshakeForPeer(t.Context(), cl.ID()))
	sent := <-got
	require.Equal(t, noDataEarliestAvailableSlot, sent.EarliestAvailableSlot)
	require.Equal(t, stored.HeadSlot, sent.HeadSlot)
	require.EqualValues(t, 32, stored.EarliestAvailableSlot, "stored status must not be mutated")
}
