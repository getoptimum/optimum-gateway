package gossipsub_gateway

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

// An inbound exchange that goes idle after the length prefix is closed by the
// whole-exchange deadline rather than held open.
func TestInboundHandlerClosesIdleStream(t *testing.T) {
	prev := reqRespStreamTimeout
	reqRespStreamTimeout = 300 * time.Millisecond
	t.Cleanup(func() { reqRespStreamTimeout = prev })

	svc, _ := newGateway(t)
	startForkGatewayHost(t, svc)
	svc.handleHandshake()

	client, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Connect(t.Context(), peer.AddrInfo{ID: svc.hostLibP2P.ID(), Addrs: svc.hostLibP2P.Addrs()}))

	stream, err := client.NewStream(t.Context(), svc.hostLibP2P.ID(), "/eth2/beacon_chain/req/ping/1/ssz_snappy")
	require.NoError(t, err)
	defer stream.Close()

	// Announce a valid 8-byte ping body, then send nothing (go idle).
	_, err = stream.Write(binary.AppendUvarint(nil, 8))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() {
		_, rerr := stream.Read(make([]byte, 1))
		done <- rerr
	}()

	select {
	case rerr := <-done:
		require.Error(t, rerr, "handler should close the idle stream, not answer it")
	case <-time.After(reqRespStreamTimeout + 3*time.Second):
		t.Fatal("idle stream was not closed within the deadline")
	}
}
