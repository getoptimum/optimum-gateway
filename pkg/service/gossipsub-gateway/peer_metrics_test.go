package gossipsub_gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

func counterSum(t *testing.T, suffix string, labels map[string]string) float64 {
	t.Helper()
	families, err := telemetry.CustomRegistry.Gather()
	require.NoError(t, err)
	var sum float64
	for _, family := range families {
		if !strings.HasSuffix(family.GetName(), suffix) {
			continue
		}
	metrics:
		for _, m := range family.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			for k, v := range labels {
				if got[k] != v {
					continue metrics
				}
			}
			sum += m.GetCounter().GetValue()
		}
	}
	return sum
}

func startIdentifiedLighthouse(t *testing.T, svc *Service) host.Host {
	t.Helper()
	client, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.UserAgent("Lighthouse/v8.3.3-test"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	require.NoError(t, client.Connect(t.Context(), peer.AddrInfo{ID: svc.hostLibP2P.ID(), Addrs: svc.hostLibP2P.Addrs()}))
	require.Eventually(t, func() bool { return svc.peerClientFamily(client.ID()) == "lighthouse" }, 5*time.Second, 10*time.Millisecond)
	return client
}

func newMetricsGateway(t *testing.T) *Service {
	t.Helper()
	svc, _ := newGateway(t)
	telemetry.InitMetrics(t.Context(), svc.log, svc.cfg)
	startForkGatewayHost(t, svc)
	svc.libP2PDirectPeers = syncx.NewRWMap[string, peer.AddrInfo]()
	return svc
}

// disconnect closes the client and runs the disconnect hook as libp2p would, once per connection.
func disconnect(t *testing.T, svc *Service, client host.Host) {
	t.Helper()
	conns := svc.hostLibP2P.Network().ConnsToPeer(client.ID())
	require.NotEmpty(t, conns)
	require.NoError(t, svc.hostLibP2P.Network().ClosePeer(client.ID()))
	for _, conn := range conns {
		svc.onPeerDisconnected(svc.hostLibP2P.Network(), conn)
	}
}

func TestDisconnectKeepsAgentForLateNotices(t *testing.T) {
	svc := newMetricsGateway(t)
	client := startIdentifiedLighthouse(t, svc)

	labels := map[string]string{"peer_client": "lighthouse"}
	before := counterSum(t, "cl_peer_disconnected_by_client_total", labels)
	disconnect(t, svc, client)
	require.Equal(t, before+1, counterSum(t, "cl_peer_disconnected_by_client_total", labels))

	require.Empty(t, svc.hostLibP2P.Peerstore().Addrs(client.ID()), "addresses go at once so pubsub can't redial")
	require.Equal(t, "lighthouse", svc.peerClientFamily(client.ID()), "a late goodbye is still attributed")
	// A second connection's notification arrives after the first already ran.
	svc.onPeerDisconnected(svc.hostLibP2P.Network(), fakeConn{remote: client.ID()})
	require.Equal(t, before+2, counterSum(t, "cl_peer_disconnected_by_client_total", labels))
}

func TestWaitIdentifiedCoversFreshConnection(t *testing.T) {
	svc := newMetricsGateway(t)
	client, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"), libp2p.UserAgent("teku/v26.10.3"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	// Connect returns once the client identified the gateway; the gateway's identify of the client may still run.
	require.NoError(t, client.Connect(t.Context(), peer.AddrInfo{ID: svc.hostLibP2P.ID(), Addrs: svc.hostLibP2P.Addrs()}))

	svc.waitIdentified(svc.hostLibP2P.Network().ConnsToPeer(client.ID())[0])
	require.Equal(t, "teku", svc.peerClientFamily(client.ID()))
}

func TestGoodbyeLabelsClient(t *testing.T) {
	svc := newMetricsGateway(t)
	svc.handleHandshake()
	client := startIdentifiedLighthouse(t, svc)

	labels := map[string]string{"peer_client": "lighthouse", "reason": "3"}
	before := counterSum(t, "cl_peer_goodbye_total", labels)
	sendGoodbye(t, svc, client, 3)
	require.Eventually(t, func() bool {
		return counterSum(t, "cl_peer_goodbye_total", labels) == before+1
	}, 5*time.Second, 10*time.Millisecond)
}

func sendGoodbye(t *testing.T, svc *Service, client host.Host, code uint64) {
	t.Helper()
	stream, err := client.NewStream(t.Context(), svc.hostLibP2P.ID(), "/eth2/beacon_chain/req/goodbye/1/ssz_snappy")
	require.NoError(t, err)
	defer stream.Close()
	reason := consensus.SSZUint64(code)
	_, err = svc.sszEncoder.EncodeWithMaxLength(stream, &reason)
	require.NoError(t, err)
	require.NoError(t, stream.CloseWrite())
	_, _ = stream.Read(make([]byte, 1)) // wait for the handler to close the stream
}

func TestRejectedPeerIsNotCounted(t *testing.T) {
	svc := newMetricsGateway(t)
	svc.handleHandshake()
	svc.directCLPeersAllowlist = map[peer.ID]struct{}{randomPeerID(t): {}}
	client := startIdentifiedLighthouse(t, svc)

	goodbyes := counterSum(t, "cl_peer_goodbye_total", nil)
	sendGoodbye(t, svc, client, 1)
	// Rejection happens before the handler closes the stream, so the count is already final.
	require.Equal(t, goodbyes, counterSum(t, "cl_peer_goodbye_total", nil))

	disconnects := counterSum(t, "cl_peer_disconnected_total", nil)
	disconnect(t, svc, client)
	require.Equal(t, disconnects, counterSum(t, "cl_peer_disconnected_total", nil))
	require.Equal(t, "unknown", svc.peerClientFamily(client.ID()), "rejected peers are removed at once")
}

type fakeConn struct {
	network.Conn
	remote peer.ID
}

func (c fakeConn) RemotePeer() peer.ID { return c.remote }

func TestDisconnectTotalStaysUnlabeled(t *testing.T) {
	svc := newMetricsGateway(t)
	client := startIdentifiedLighthouse(t, svc)
	disconnect(t, svc, client)

	families, err := telemetry.CustomRegistry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if strings.HasSuffix(family.GetName(), "cl_peer_disconnected_total") {
			for _, m := range family.GetMetric() {
				for _, lp := range m.GetLabel() {
					require.NotEqual(t, "peer_client", lp.GetName(), "existing queries pair it with cl_peer_connected_total")
				}
			}
		}
	}
}

func TestSetupRejectsBadDirectPeerBeforeListening(t *testing.T) {
	svc, _ := newGateway(t)
	svc.cfg.IdentityLibP2PDir = t.TempDir()
	svc.cfg.DirectCLPeers = []string{"not-a-multiaddr"}

	err := svc.setupLibP2PHost()
	require.ErrorContains(t, err, "direct CL peers")
	require.Nil(t, svc.hostLibP2P, "the allowlist must be built before the host starts serving handlers")
}

func gaugeSum(t *testing.T, suffix string) float64 {
	t.Helper()
	families, err := telemetry.CustomRegistry.Gather()
	require.NoError(t, err)
	var sum float64
	for _, family := range families {
		if strings.HasSuffix(family.GetName(), suffix) {
			for _, m := range family.GetMetric() {
				sum += m.GetGauge().GetValue()
			}
		}
	}
	return sum
}

func TestRejectedPeerThroughNotifieeLeavesGaugeBalanced(t *testing.T) {
	svc := newMetricsGateway(t)
	svc.directCLPeersAllowlist = map[peer.ID]struct{}{randomPeerID(t): {}}
	svc.hostLibP2P.Network().Notify(&network.NotifyBundle{
		ConnectedF:    svc.onPeerConnected,
		DisconnectedF: svc.onPeerDisconnected,
	})
	peers := gaugeSum(t, "_cl_peers")
	disconnects := counterSum(t, "cl_peer_disconnected_total", nil)

	stranger, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = stranger.Close() })
	_ = stranger.Connect(t.Context(), peer.AddrInfo{ID: svc.hostLibP2P.ID(), Addrs: svc.hostLibP2P.Addrs()})
	require.Eventually(t, func() bool {
		return svc.hostLibP2P.Network().Connectedness(stranger.ID()) != network.Connected
	}, 5*time.Second, 10*time.Millisecond)

	require.Never(t, func() bool {
		return gaugeSum(t, "_cl_peers") != peers || counterSum(t, "cl_peer_disconnected_total", nil) != disconnects
	}, 500*time.Millisecond, 20*time.Millisecond)
}
