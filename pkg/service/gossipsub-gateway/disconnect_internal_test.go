package gossipsub_gateway

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/syncx"
)

func TestDisconnectPreservesConfiguredCLPeerForRedial(t *testing.T) {
	for _, configured := range []bool{true, false} {
		name := "discovered peer"
		if configured {
			name = "configured peer"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			gateway, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, gateway.Close()) })
			cl, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, cl.Close()) })
			info := peer.AddrInfo{ID: cl.ID(), Addrs: cl.Addrs()}
			svc := &Service{
				hostLibP2P:             gateway,
				libP2PDirectPeers:      syncx.NewRWMap[string, peer.AddrInfo](),
				directCLPeersAllowlist: make(map[peer.ID]struct{}),
			}
			if configured {
				svc.directCLPeersAllowlist[cl.ID()] = struct{}{}
				// Use the production option to install the permanent dial address.
				params := pubsub.DefaultGossipSubParams()
				params.DirectConnectInitialDelay = time.Hour
				params.DirectConnectTicks = 1_000_000
				_, err = pubsub.NewGossipSub(ctx, gateway,
					pubsub.WithDirectPeers([]peer.AddrInfo{info}),
					pubsub.WithGossipSubParams(params),
				)
				require.NoError(t, err)
			}
			require.NoError(t, gateway.Connect(ctx, info))
			svc.libP2PDirectPeers.Store(cl.ID().String(), info)
			connections := gateway.Network().ConnsToPeer(cl.ID())
			require.NotEmpty(t, connections)
			conn := connections[0]
			svc.onPeerDisconnected(gateway.Network(), conn)
			_, present := svc.libP2PDirectPeers.Load(cl.ID().String())
			require.True(t, present, "a peer with a remaining connection must stay in the live list")

			require.NoError(t, gateway.Network().ClosePeer(cl.ID()))
			require.Eventually(t, func() bool {
				return gateway.Network().Connectedness(cl.ID()) != network.Connected
			}, time.Second, time.Millisecond)
			svc.onPeerDisconnected(gateway.Network(), conn)
			_, present = svc.libP2PDirectPeers.Load(cl.ID().String())
			require.False(t, present, "disconnected peers must leave the live peer list")

			if !configured {
				require.Empty(t, gateway.Peerstore().Addrs(cl.ID()))
				return
			}
			require.Subset(t, gateway.Peerstore().Addrs(cl.ID()), info.Addrs,
				"configured dial addresses must survive a disconnect")
			// GossipSub's reconnect loop dials by ID, without supplying addresses.
			require.NoError(t, gateway.Connect(ctx, peer.AddrInfo{ID: cl.ID()}))
			require.Equal(t, network.Connected, gateway.Network().Connectedness(cl.ID()))
		})
	}
}
