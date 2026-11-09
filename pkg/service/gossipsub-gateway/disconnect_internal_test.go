package gossipsub_gateway

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/syncx"
)

func TestDisconnectPreservesConfiguredCLPeerForRedial(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	gateway, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, gateway.Close()) })
	cl, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cl.Close()) })

	// The configured dial address is not the address of the live connection.
	configured := multiaddr.StringCast("/ip4/127.0.0.1/tcp/13000")
	gateway.Peerstore().AddAddr(cl.ID(), configured, peerstore.PermanentAddrTTL)
	gateway.Peerstore().AddAddrs(cl.ID(), cl.Addrs(), peerstore.PermanentAddrTTL)

	svc := &Service{
		hostLibP2P:             gateway,
		libP2PDirectPeers:      syncx.NewRWMap[string, peer.AddrInfo](),
		directCLPeersAllowlist: map[peer.ID]struct{}{cl.ID(): {}},
	}
	require.NoError(t, gateway.Connect(ctx, peer.AddrInfo{ID: cl.ID(), Addrs: cl.Addrs()}))
	conn := gateway.Network().ConnsToPeer(cl.ID())[0]
	svc.libP2PDirectPeers.Store(cl.ID().String(), peer.AddrInfo{ID: cl.ID()})

	svc.onPeerDisconnected(gateway.Network(), conn)
	_, present := svc.libP2PDirectPeers.Load(cl.ID().String())
	require.True(t, present, "a peer with a remaining connection stays in the live list")

	require.NoError(t, gateway.Network().ClosePeer(cl.ID()))
	require.Eventually(t, func() bool {
		return gateway.Network().Connectedness(cl.ID()) != network.Connected
	}, time.Second, time.Millisecond)
	// Identify demotes connection addresses to a short TTL, then deletes those.
	gateway.Peerstore().UpdateAddrs(cl.ID(), peerstore.ConnectedAddrTTL, peerstore.TempAddrTTL)
	gateway.Peerstore().UpdateAddrs(cl.ID(), peerstore.TempAddrTTL, 0)
	svc.onPeerDisconnected(gateway.Network(), conn)

	_, present = svc.libP2PDirectPeers.Load(cl.ID().String())
	require.False(t, present, "a disconnected peer leaves the live list")
	require.Contains(t, addrStrings(gateway.Peerstore().Addrs(cl.ID())), configured.String())
	require.NoError(t, gateway.Connect(ctx, peer.AddrInfo{ID: cl.ID()}))

	delete(svc.directCLPeersAllowlist, cl.ID())
	require.NoError(t, gateway.Network().ClosePeer(cl.ID()))
	require.Eventually(t, func() bool {
		return gateway.Network().Connectedness(cl.ID()) != network.Connected
	}, time.Second, time.Millisecond)
	svc.onPeerDisconnected(gateway.Network(), conn)
	require.Empty(t, gateway.Peerstore().Addrs(cl.ID()), "a peer that is not configured is cleared")
}

func addrStrings(addrs []multiaddr.Multiaddr) []string {
	out := make([]string, len(addrs))
	for i, addr := range addrs {
		out[i] = addr.String()
	}
	return out
}
