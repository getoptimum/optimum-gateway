package mum_p2p_test

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	commontest "github.com/getoptimum/optimum-common/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// Simulates a dedicated TCP passthrough load balancer in front of one gateway.
func startTCPForwarder(t *testing.T, target string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			in, err := ln.Accept()
			if err != nil {
				return
			}
			out, err := net.Dial("tcp", target)
			if err != nil {
				_ = in.Close()
				continue
			}
			go func() { _, _ = io.Copy(out, in); _ = out.Close() }()
			go func() { _, _ = io.Copy(in, out); _ = in.Close() }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestNodeReachableThroughAnnouncedAddr(t *testing.T) {
	cnt := test_utils.GetClean(t)
	clusterID := "optimum_test_announce"

	listenPort := commontest.GetFreePortT(t)
	lbPort := startTCPForwarder(t, fmt.Sprintf("127.0.0.1:%d", listenPort))
	announced := multiaddr.StringCast(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", lbPort))

	cfgA := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, listenPort, nil)
	cfgA.AdvertisedAddrs = []multiaddr.Multiaddr{announced}
	nodeA, err := mum_p2p.NewNode(cnt.Ctx, cnt.Log, cfgA, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(nodeA.Stop)

	infoA := nodeA.GetHostInfo()
	require.Equal(t, []multiaddr.Multiaddr{announced}, infoA.Addrs)

	nodeB := test_utils.NewTestNode(cnt.Ctx, t, cnt.Log, clusterID, t.TempDir())
	require.NoError(t, nodeB.GetHost().Connect(cnt.Ctx, infoA))

	const topic = "beacon_block"
	require.NoError(t, nodeA.SubscribeTopic(topic))
	require.NoError(t, nodeB.SubscribeTopic(topic))
	require.Eventually(t, func() bool {
		_, perTopicPeers := nodeB.CountConnectedPeers()
		return perTopicPeers[topic] > 0
	}, 10*time.Second, 100*time.Millisecond)

	listener := nodeA.RegisterListener("announce")
	t.Cleanup(func() { nodeA.UnregisterListener("announce") })
	payload := []byte("via load balancer")
	require.NoError(t, nodeB.PublishMessage(cnt.Ctx, topic, payload))
	select {
	case msg := <-listener:
		require.Equal(t, payload, msg.Message.Message)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for message through announced address")
	}
}
