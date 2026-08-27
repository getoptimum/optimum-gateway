package mum_p2p

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commonlogger "github.com/getoptimum/optimum-common/pkg/logger"
	cfgpkg "github.com/getoptimum/optimum-gateway/pkg/config"
)

func TestNewNodeAcceptsInboundTCPAndQUIC(t *testing.T) {
	// NewNode normally discovers public addresses. Keep this production
	// constructor test deterministic while still exercising its host setup.
	oldGetExternalIPs := getExternalIPs
	getExternalIPs = func() (string, string, error) { return "127.0.0.1", "", nil }
	t.Cleanup(func() { getExternalIPs = oldGetExternalIPs })

	for _, tc := range []struct {
		name      string
		transport string
		addr      string
		dialOpts  []libp2p.Option
	}{
		{
			name:      "tcp",
			transport: "tcp",
			addr:      "/ip4/127.0.0.1/tcp/%d/p2p/%s",
			dialOpts:  []libp2p.Option{libp2p.Transport(tcp.NewTCPTransport)},
		},
		{
			name:      "quic",
			transport: "quic-v1",
			addr:      "/ip4/127.0.0.1/udp/%d/quic-v1/p2p/%s",
			dialOpts:  []libp2p.Option{libp2p.Transport(libp2pquic.NewTransport)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			log := commonlogger.NewAppSLogger(commonlogger.Error)
			cfg := newTransportTestConfig(ctx, log, "transport-test-"+tc.name)

			target, err := NewNode(ctx, log, cfg, t.TempDir())
			require.NoError(t, err)
			t.Cleanup(target.Stop)

			dialOpts := append([]libp2p.Option{}, tc.dialOpts...)
			dialer, err := libp2p.New(dialOpts...)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, dialer.Close()) })

			addr := multiaddr.StringCast(fmt.Sprintf(tc.addr, cfg.ListenPort, target.GetHost().ID().String()))
			require.NoError(t, dialer.Connect(ctx, peer.AddrInfo{ID: target.GetHost().ID(), Addrs: []multiaddr.Multiaddr{addr}}))

			require.Eventually(t, func() bool {
				for _, conn := range target.GetHost().Network().ConnsToPeer(dialer.ID()) {
					state := conn.ConnState()
					if state.Transport == tc.transport && conn.Stat().Direction == network.DirInbound {
						return true
					}
				}
				return false
			}, 5*time.Second, 25*time.Millisecond)
		})
	}
}

func newTransportTestConfig(ctx context.Context, log commonlogger.AppLogger, clusterID string) *Config {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return &Config{
		ClusterID:      clusterID,
		ListenPort:     port,
		MaxMessageSize: cfgpkg.DefaultMaxMessageSize,
		Rotator: commonconfig.NewConfigRotator(ctx, log, &commonentities.OptimumConfig{
			MaxMessageSize: cfgpkg.DefaultMaxMessageSize,
		}, "hoodi", clusterID, func(*commonentities.DynamicConfig) {}),
	}
}
