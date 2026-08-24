package telemetry

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func TestConnectionsMeeter(t *testing.T) {
	reg := initTestMetricsRegistry(t, initConnMetrics)
	meter := NewConnectionsMeeter()
	require.NotNil(t, meter)

	hostA, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, hostA.Close())
	})

	hostB, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, hostB.Close())
	})

	testAddr, err := multiaddr.NewMultiaddr("/ip4/127.0.0.1/tcp/9999")
	require.NoError(t, err)
	meter.Listen(nil, testAddr)
	meter.ListenClose(nil, testAddr)

	require.NoError(t, hostA.Connect(t.Context(), peer.AddrInfo{
		ID:    hostB.ID(),
		Addrs: hostB.Addrs(),
	}))

	var conn network.Conn
	require.Eventually(t, func() bool {
		conns := hostA.Network().ConnsToPeer(hostB.ID())
		if len(conns) == 0 {
			return false
		}
		conn = conns[0]
		return true
	}, 5*time.Second, 20*time.Millisecond)

	meter.Connected(nil, conn)
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_conn_total_connections",
		map[string]string{labelDirection: "outbound"},
	).GetGauge().GetValue())

	meter.Disconnected(nil, conn)
	require.Equal(t, float64(0), metricByLabels(t, reg,
		testMetricsNamespace+"_conn_total_connections",
		map[string]string{labelDirection: "outbound"},
	).GetGauge().GetValue())
}
