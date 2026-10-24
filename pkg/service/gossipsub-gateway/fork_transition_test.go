package gossipsub_gateway

import (
	"bytes"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
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
	forkTestCurrentDigest = "deadbeef"
	forkTestFutureDigest  = "ddeeff00"
)

type forkTransitionCL struct {
	ps       *libp2ppubsub.PubSub
	mu       sync.Mutex
	topics   map[string]*libp2ppubsub.Topic
	received map[string][][]byte
}

type forkTransitionEventTracer struct {
	peerTopics *tracer.PeerTopicTracer
	grafted    chan string
}

func (f *forkTransitionEventTracer) Trace(event *pubsubpb.TraceEvent) {
	f.peerTopics.Trace(event)
	if event == nil || event.GetType() != pubsubpb.TraceEvent_GRAFT {
		return
	}
	graft := event.GetGraft()
	if graft == nil || graft.GetTopic() == "" {
		return
	}
	select {
	case f.grafted <- graft.GetTopic():
	default:
	}
}

func startForkTransitionGatewayHost(t *testing.T, svc *Service) *forkTransitionEventTracer {
	t.Helper()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	svc.hostLibP2P = h
	svc.peerTopicTracer = tracer.NewPeerTopicTracer(svc.log, svc.GetForkDigestManager(), svc.topicDiscovered)
	eventTracer := &forkTransitionEventTracer{
		peerTopics: svc.peerTopicTracer,
		grafted:    make(chan string, 128),
	}
	svc.ps, err = libp2ppubsub.NewGossipSub(svc.ctx, h,
		libp2ppubsub.WithMessageSignaturePolicy(libp2ppubsub.StrictNoSign),
		libp2ppubsub.WithNoAuthor(),
		libp2ppubsub.WithSeenMessagesTTL(gossipSubSeenTTL),
		libp2ppubsub.WithMessageIdFn(svc.pubsubMsgID),
		libp2ppubsub.WithGossipSubParams(pubsubGossipParam()),
		libp2ppubsub.WithPeerExchange(false),
		libp2ppubsub.WithMaxMessageSize(consensus.MaxCompressedGossipSize()),
		libp2ppubsub.WithEventTracer(eventTracer),
	)
	require.NoError(t, err)
	return eventTracer
}

func startForkTransitionCL(t *testing.T, svc *Service) *forkTransitionCL {
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
	require.NoError(t, h.Connect(t.Context(), peer.AddrInfo{
		ID:    svc.hostLibP2P.ID(),
		Addrs: svc.hostLibP2P.Addrs(),
	}))

	return &forkTransitionCL{
		ps:       ps,
		topics:   make(map[string]*libp2ppubsub.Topic),
		received: make(map[string][][]byte),
	}
}

func (c *forkTransitionCL) subscribe(t *testing.T, topic string) {
	t.Helper()
	tp, err := c.ps.Join(topic)
	require.NoError(t, err)
	sub, err := tp.Subscribe()
	require.NoError(t, err)
	t.Cleanup(func() {
		sub.Cancel()
		_ = tp.Close()
	})

	c.mu.Lock()
	c.topics[topic] = tp
	c.mu.Unlock()
	go func() {
		for {
			msg, err := sub.Next(t.Context())
			if err != nil {
				return
			}
			data := append([]byte(nil), msg.Data...)
			c.mu.Lock()
			c.received[topic] = append(c.received[topic], data)
			c.mu.Unlock()
		}
	}()
}

func (c *forkTransitionCL) publish(t *testing.T, topic string, data []byte) {
	t.Helper()
	c.mu.Lock()
	tp := c.topics[topic]
	c.mu.Unlock()
	require.NotNil(t, tp)
	require.NoError(t, tp.Publish(t.Context(), data))
}

func (c *forkTransitionCL) countPayload(topic string, payload []byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, received := range c.received[topic] {
		if bytes.Equal(received, payload) {
			count++
		}
	}
	return count
}

func forkTransitionAttestation(t *testing.T, attester uint64) ([]byte, []byte) {
	t.Helper()
	att := &consensus.SingleAttestation{
		CommitteeIndex: 1,
		AttesterIndex:  consensus.ValidatorIndex(attester),
		Data: consensus.AttestationData{
			Slot:            100,
			CommitteeIndex:  0,
			BeaconBlockRoot: test_utils.Arr32(1),
			Source:          consensus.Checkpoint{Epoch: 2, Root: test_utils.Arr32(2)},
			Target:          consensus.Checkpoint{Epoch: 3, Root: test_utils.Arr32(3)},
		},
		Signature: test_utils.Arr96(byte(attester)),
	}
	encoded := test_utils.SSZSnappyEncode(t, att)
	packer := aggregator.NewAttestationPacker(func(uint64) bool { return true })
	sourceTopic := topics.BuildFullTopic(forkTestCurrentDigest, "beacon_attestation_5")
	require.NoError(t, packer.Add(topics.ParseTopicMeta(sourceTopic), encoded))
	packed, err := packer.EncodeCurrent()
	require.NoError(t, err)
	require.Len(t, packed, 1)
	return packed[0], encoded
}

func truncatedForkTransitionBlock(t *testing.T) []byte {
	t.Helper()
	encoded, err := hex.DecodeString(strings.TrimSpace(test_utils.ValidBeaconBlockMessage))
	require.NoError(t, err)
	raw, err := (&consensus.SSZSnappyCodec{}).DecodeGossipRaw(encoded)
	require.NoError(t, err)

	// SignedBeaconBlock prefix (4-byte offset + 96-byte signature) and the
	// 112-byte header are enough for DecodeBeaconBlockHeader, but omit the body.
	const headerOnlyLen = 4 + 96 + 112
	require.GreaterOrEqual(t, len(raw), headerOnlyLen)
	truncated := snappy.Encode(nil, raw[:headerOnlyLen])
	_, err = consensus.DecodeBeaconBlockHeader(truncated)
	require.NoError(t, err)
	return truncated
}

func deliverForkTransitionAttestation(t *testing.T, svc *Service, attester uint64) []byte {
	t.Helper()
	packed, encoded := forkTransitionAttestation(t, attester)
	svc.mumP2PMessages <- &commonentities.P2PMessage{
		SourceNodeID: testPeerID,
		Topic:        mumP2PAggregatedMessagesTopic,
		Message:      packed,
	}
	return encoded
}

func requireForkTransitionCLMessage(t *testing.T, svc *Service, topic string, payload []byte) {
	t.Helper()
	require.Eventually(t, func() bool {
		select {
		case msg := <-svc.clMessages:
			return msg.Topic == topic && bytes.Equal(msg.Message, payload)
		default:
			return false
		}
	}, 10*time.Second, 50*time.Millisecond)
}

func requireNoForkTransitionPayload(t *testing.T, cl *forkTransitionCL, topic string, payload []byte) {
	t.Helper()
	require.Never(t, func() bool {
		return cl.countPayload(topic, payload) > 0
	}, 2*gossipSubHeartbeatInterval, 50*time.Millisecond)
}

func requireForkTransitionMesh(t *testing.T, eventTracer *forkTransitionEventTracer, topic string) {
	t.Helper()
	require.Eventually(t, func() bool {
		select {
		case graftedTopic := <-eventTracer.grafted:
			return graftedTopic == topic
		default:
			return false
		}
	}, 10*time.Second, 50*time.Millisecond)
}

// TestForkTransitionAggregatedAttestations extends the #186 reproduction shared
// by its reporter across both sides of the fork transition.
func TestForkTransitionAggregatedAttestations(t *testing.T) {
	svc, _ := newGateway(t)
	t.Cleanup(svc.messagesMap.Close)
	t.Cleanup(svc.streamDedup.Close)
	forkManager := svc.GetForkDigestManager()
	require.True(t, forkManager.CheckForkSupported(forkTestCurrentDigest))
	require.True(t, forkManager.CheckForkSupported(forkTestFutureDigest))

	eventTracer := startForkTransitionGatewayHost(t, svc)
	svc.subscribeOnce()
	cl := startForkTransitionCL(t, svc)

	currentTopic := topics.BuildFullTopic(forkTestCurrentDigest, "beacon_attestation_5")
	futureTopic := topics.BuildFullTopic(forkTestFutureDigest, "beacon_attestation_5")
	futureBlockTopic := topics.BuildFullTopic(forkTestFutureDigest, "beacon_block")
	cl.subscribe(t, currentTopic)
	go svc.handleMessagesFromMumP2PNode()

	require.Eventually(t, func() bool {
		return len(svc.ps.ListPeers(currentTopic)) == 1 && len(cl.ps.ListPeers(currentTopic)) == 1
	}, 10*time.Second, 50*time.Millisecond)
	requireForkTransitionMesh(t, eventTracer, currentTopic)

	baseline := deliverForkTransitionAttestation(t, svc, 1)
	require.Eventually(t, func() bool {
		return cl.countPayload(currentTopic, baseline) == 1
	}, 10*time.Second, 50*time.Millisecond)
	require.Zero(t, cl.countPayload(futureTopic, baseline))

	// A pre-fork subscription prepares the future topic without activating it.
	cl.subscribe(t, futureTopic)
	cl.subscribe(t, futureBlockTopic)
	require.Eventually(t, func() bool {
		_, joinedAttestation := svc.libP2PTopics.Load(futureTopic)
		_, joinedBlock := svc.libP2PTopics.Load(futureBlockTopic)
		return joinedAttestation && joinedBlock &&
			len(svc.ps.ListPeers(futureTopic)) == 1 && len(cl.ps.ListPeers(futureTopic)) == 1 &&
			len(svc.ps.ListPeers(futureBlockTopic)) == 1 && len(cl.ps.ListPeers(futureBlockTopic)) == 1
	}, 10*time.Second, 50*time.Millisecond)
	requireForkTransitionMesh(t, eventTracer, futureBlockTopic)
	require.Equal(t, forkTestCurrentDigest, forkManager.ActiveDigest())

	preFork := deliverForkTransitionAttestation(t, svc, 2)
	require.Eventually(t, func() bool {
		return cl.countPayload(currentTopic, preFork) == 1
	}, 10*time.Second, 50*time.Millisecond)
	requireNoForkTransitionPayload(t, cl, futureTopic, preFork)

	// Periodic reconciliation must not make the future digest active either.
	svc.refreshTopics()
	stillPreFork := deliverForkTransitionAttestation(t, svc, 3)
	require.Eventually(t, func() bool {
		return cl.countPayload(currentTopic, stillPreFork) == 1
	}, 10*time.Second, 50*time.Millisecond)
	requireNoForkTransitionPayload(t, cl, futureTopic, stillPreFork)
	require.Equal(t, forkTestCurrentDigest, forkManager.ActiveDigest())

	// A header-only future block cannot activate the fork. Blocks retain their
	// source topic, so attestation traffic is the activation signal.
	truncatedFutureBlock := truncatedForkTransitionBlock(t)
	cl.publish(t, futureBlockTopic, truncatedFutureBlock)
	requireForkTransitionCLMessage(t, svc, futureBlockTopic, truncatedFutureBlock)
	require.Equal(t, forkTestCurrentDigest, forkManager.ActiveDigest())

	// Malformed traffic cannot advance gateway-wide fork state.
	malformedFuture := []byte("not-valid-ssz-snappy")
	cl.publish(t, futureTopic, malformedFuture)
	requireForkTransitionCLMessage(t, svc, futureTopic, malformedFuture)
	require.Equal(t, forkTestCurrentDigest, forkManager.ActiveDigest())

	// Actual CL traffic on the configured future topic activates the new digest.
	_, futureActivation := forkTransitionAttestation(t, 4)
	cl.publish(t, futureTopic, futureActivation)
	require.Eventually(t, func() bool {
		return forkManager.ActiveDigest() == forkTestFutureDigest
	}, 10*time.Second, 50*time.Millisecond)
	requireForkTransitionCLMessage(t, svc, futureTopic, futureActivation)

	postFork := deliverForkTransitionAttestation(t, svc, 5)
	require.Eventually(t, func() bool {
		return cl.countPayload(futureTopic, postFork) == 1
	}, 10*time.Second, 50*time.Millisecond)
	requireNoForkTransitionPayload(t, cl, currentTopic, postFork)

	// A lagging message on the old topic cannot roll the active digest back.
	_, laggingCurrent := forkTransitionAttestation(t, 6)
	cl.publish(t, currentTopic, laggingCurrent)
	requireForkTransitionCLMessage(t, svc, currentTopic, laggingCurrent)
	require.Equal(t, forkTestFutureDigest, forkManager.ActiveDigest())

	afterLaggingMessage := deliverForkTransitionAttestation(t, svc, 7)
	require.Eventually(t, func() bool {
		return cl.countPayload(futureTopic, afterLaggingMessage) == 1
	}, 10*time.Second, 50*time.Millisecond)
	requireNoForkTransitionPayload(t, cl, currentTopic, afterLaggingMessage)
}
