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
	// NewNode calls GetExternalIPs for advertised addrs. Stub it so this
	// only exercises listen + transport registration.
	oldGetExternalIPs := getExternalIPs
	getExternalIPs = func() (string, string, error) { return "127.0.0.1", "", nil }
	t.Cleanup(func() { getExternalIPs = oldGetExternalIPs })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	log := commonlogger.NewAppSLogger(commonlogger.Error)
	cfg := newTransportTestConfig(t, ctx, log)

	target, err := NewNode(ctx, log, cfg, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(target.Stop)

	id := target.GetHost().ID()
	port := cfg.ListenPort

	dial := func(t *testing.T, transport, addrFmt string, opts ...libp2p.Option) {
		t.Helper()
		dialer, err := libp2p.New(opts...)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, dialer.Close()) })

		addr := multiaddr.StringCast(fmt.Sprintf(addrFmt, port, id))
		require.NoError(t, dialer.Connect(ctx, peer.AddrInfo{ID: id, Addrs: []multiaddr.Multiaddr{addr}}))

		conns := target.GetHost().Network().ConnsToPeer(dialer.ID())
		require.NotEmpty(t, conns)
		require.Equal(t, transport, conns[0].ConnState().Transport)
		require.Equal(t, network.DirInbound, conns[0].Stat().Direction)
	}

	t.Run("tcp", func(t *testing.T) {
		dial(t, "tcp", "/ip4/127.0.0.1/tcp/%d/p2p/%s", libp2p.Transport(tcp.NewTCPTransport))
	})
	t.Run("quic", func(t *testing.T) {
		dial(t, "quic-v1", "/ip4/127.0.0.1/udp/%d/quic-v1/p2p/%s", libp2p.Transport(libp2pquic.NewTransport))
	})
}

func newTransportTestConfig(t *testing.T, ctx context.Context, log commonlogger.AppLogger) *Config {
	t.Helper()
	return &Config{
		ClusterID:      "transport-test",
		ListenPort:     freeTCPUDPPort(t),
		MaxMessageSize: cfgpkg.DefaultMaxMessageSize,
		Rotator: commonconfig.NewConfigRotator(ctx, log, &commonentities.OptimumConfig{
			MaxMessageSize: cfgpkg.DefaultMaxMessageSize,
		}, "hoodi", "transport-test", func(*commonentities.DynamicConfig) {}),
	}
}

func freeTCPUDPPort(t *testing.T) int {
	t.Helper()
	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := ln.Addr().(*net.TCPAddr).Port
		pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			_ = ln.Close()
			continue
		}
		require.NoError(t, ln.Close())
		require.NoError(t, pc.Close())
		return port
	}
	t.Fatal("could not allocate a port free on both TCP and UDP")
	return 0
}
