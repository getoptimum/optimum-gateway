package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
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

	const protoID = protocol.ID("/optimum/test/1.0.0")
	hostB.SetStreamHandler(protoID, func(s network.Stream) {
		<-t.Context().Done()
		_ = s.Close()
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

	stream, err := hostA.NewStream(context.Background(), hostB.ID(), protoID)
	require.NoError(t, err)
	defer stream.Close()

	meter.OpenedStream(nil, stream)
	require.Equal(t, float64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_conn_streams_current",
		map[string]string{labelProtocol: string(protoID)},
	).GetGauge().GetValue())

	time.Sleep(10 * time.Millisecond)
	meter.ClosedStream(nil, stream)
	require.Equal(t, float64(0), metricByLabels(t, reg,
		testMetricsNamespace+"_conn_streams_current",
		map[string]string{labelProtocol: string(protoID)},
	).GetGauge().GetValue())
	require.Equal(t, uint64(1), metricByLabels(t, reg,
		testMetricsNamespace+"_conn_stream_duration_seconds",
		map[string]string{labelProtocol: string(protoID)},
	).GetHistogram().GetSampleCount())

	meter.Disconnected(nil, conn)
	require.Equal(t, float64(0), metricByLabels(t, reg,
		testMetricsNamespace+"_conn_total_connections",
		map[string]string{labelDirection: "outbound"},
	).GetGauge().GetValue())
}
