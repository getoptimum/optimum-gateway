package gossipsub_gateway

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/pointers"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/aggregator"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

const (
	forkCurDigest  = "deadbeef" // bootstrap fork_digest in newGateway
	forkNextDigest = "ddeeff00" // bootstrap future_fork in newGateway
)

// startForkGatewayHost builds the gateway CL host with the same pubsub options as setupLibP2PHost.
func startForkGatewayHost(t *testing.T, svc *Service) {
	t.Helper()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	svc.hostLibP2P = h
	svc.peerTopicTracer = tracer.NewPeerTopicTracer(svc.log, svc.GetForkDigestManager(), svc.onZeroPeersForTopic)
	svc.ps, err = libp2ppubsub.NewGossipSub(svc.ctx, h,
		libp2ppubsub.WithMessageSignaturePolicy(libp2ppubsub.StrictNoSign),
		libp2ppubsub.WithNoAuthor(),
		libp2ppubsub.WithSeenMessagesTTL(gossipSubSeenTTL),
		libp2ppubsub.WithMessageIdFn(svc.pubsubMsgID),
		libp2ppubsub.WithGossipSubParams(pubsubGossipParam()),
		libp2ppubsub.WithPeerExchange(false),
		libp2ppubsub.WithMaxMessageSize(consensus.MaxCompressedGossipSize()),
		libp2ppubsub.WithEventTracer(svc.peerTopicTracer),
	)
	require.NoError(t, err)
}

type forkCL struct {
	self peer.ID
	ps   *libp2ppubsub.PubSub
	mu   sync.Mutex
	recv map[string]int
}

func startForkCL(t *testing.T, svc *Service) *forkCL {
	t.Helper()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	ps, err := libp2ppubsub.NewGossipSub(t.Context(), h,
		libp2ppubsub.WithMessageSignaturePolicy(libp2ppubsub.StrictNoSign),
		libp2ppubsub.WithNoAuthor(),
		libp2ppubsub.WithMessageIdFn(func(m *pubsubpb.Message) string {
			return consensus.MsgID(pointers.FromPointer(m.Topic), m.Data)
		}),
	)
	require.NoError(t, err)
	require.NoError(t, h.Connect(t.Context(), peer.AddrInfo{ID: svc.hostLibP2P.ID(), Addrs: svc.hostLibP2P.Addrs()}))
	return &forkCL{self: h.ID(), ps: ps, recv: map[string]int{}}
}

func (c *forkCL) subscribe(t *testing.T, topic string) *libp2ppubsub.Topic {
	t.Helper()
	tp, err := c.ps.Join(topic)
	require.NoError(t, err)
	sub, err := tp.Subscribe()
	require.NoError(t, err)
	go func() {
		for {
			msg, err := sub.Next(t.Context())
			if err != nil {
				return
			}
			if msg.GetFrom() == c.self || msg.ReceivedFrom == c.self {
				continue
			}
			c.mu.Lock()
			c.recv[topic]++
			c.mu.Unlock()
		}
	}()
	return tp
}

func (c *forkCL) count(topic string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recv[topic]
}

func packedForkAttestation(t *testing.T, attester uint64) []byte {
	t.Helper()
	att := &consensus.SingleAttestation{
		CommitteeIndex: 1,
		AttesterIndex:  consensus.ValidatorIndex(attester),
		Data: consensus.AttestationData{
			Slot: 100, CommitteeIndex: 0, BeaconBlockRoot: test_utils.Arr32(1),
			Source: consensus.Checkpoint{Epoch: 2, Root: test_utils.Arr32(2)},
			Target: consensus.Checkpoint{Epoch: 3, Root: test_utils.Arr32(3)},
		},
		Signature: test_utils.Arr96(byte(attester)),
	}
	packer := aggregator.NewAttestationPacker(func(uint64) bool { return true })
	srcTopic := topics.BuildFullTopic(forkCurDigest, "beacon_attestation_5")
	require.NoError(t, packer.Add(topics.ParseTopicMeta(srcTopic), test_utils.SSZSnappyEncode(t, att)))
	out, err := packer.EncodeCurrent()
	require.NoError(t, err)
	require.Len(t, out, 1)
	return out[0]
}

func waitForkMesh(t *testing.T, svc *Service, cl *forkCL, topic string) {
	t.Helper()
	require.Eventually(t, func() bool { return len(svc.ps.ListPeers(topic)) == 1 }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { return len(cl.ps.ListPeers(topic)) == 1 }, 10*time.Second, 50*time.Millisecond)
	time.Sleep(2 * time.Second) // one heartbeat past graft, so the first publish is forwarded
}

func TestForkTransitionAggregatedAttestations(t *testing.T) {
	svc, _ := newGateway(t)
	fm := svc.GetForkDigestManager()
	require.True(t, fm.CheckForkSupported(forkCurDigest))
	require.True(t, fm.CheckForkSupported(forkNextDigest))

	startForkGatewayHost(t, svc)
	svc.subscribeOnce()
	go svc.handleMessagesFromMumP2PNode()
	cl := startForkCL(t, svc)

	oldTopic := topics.BuildFullTopic(forkCurDigest, "beacon_attestation_5")
	newTopic := topics.BuildFullTopic(forkNextDigest, "beacon_attestation_5")
	oldTp := cl.subscribe(t, oldTopic)
	waitForkMesh(t, svc, cl, oldTopic)

	deliver := func(attester uint64) {
		svc.mumP2PMessages <- &commonentities.P2PMessage{
			SourceNodeID: testPeerID,
			Topic:        mumP2PAggregatedMessagesTopic,
			Message:      packedForkAttestation(t, attester),
		}
	}
	waitCount := func(topic string, n int) {
		require.Eventually(t, func() bool { return cl.count(topic) == n }, 10*time.Second, 50*time.Millisecond)
	}

	deliver(1)
	waitCount(oldTopic, 1)
	require.Equal(t, 0, cl.count(newTopic))

	newTp := cl.subscribe(t, newTopic)
	require.Eventually(t, func() bool {
		return slices.Contains(svc.peerTopicTracer.GetDiscoveredTopics(), newTopic)
	}, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, forkCurDigest, fm.ActiveDigest())

	svc.refreshTopics()
	waitForkMesh(t, svc, cl, newTopic)
	deliver(2)
	waitCount(oldTopic, 2)
	require.Equal(t, 0, cl.count(newTopic))
	require.Equal(t, forkCurDigest, fm.ActiveDigest())

	require.NoError(t, newTp.Publish(t.Context(), []byte{1}))
	require.Eventually(t, func() bool { return fm.ActiveDigest() == forkNextDigest }, 10*time.Second, 50*time.Millisecond)

	deliver(3)
	waitCount(newTopic, 1)
	require.Equal(t, 2, cl.count(oldTopic))

	require.NoError(t, oldTp.Publish(t.Context(), []byte{2}))
	require.Never(t, func() bool { return fm.ActiveDigest() != forkNextDigest }, time.Second, 50*time.Millisecond)

	deliver(4)
	waitCount(newTopic, 2)
	require.Equal(t, 2, cl.count(oldTopic))
}
