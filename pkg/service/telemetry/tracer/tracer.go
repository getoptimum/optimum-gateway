package tracer

import (
	"fmt"
	"sync"

	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonmaps "github.com/getoptimum/optimum-common/pkg/maps"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// PeerTopicTracer implements libp2p pubsub EventTracer to discover topics from connected peers
type PeerTopicTracer struct {
	log               logger.AppLogger
	discoveredTopics  map[string]map[peer.ID]struct{} // topic -> set of peer IDs
	unsubscribeTopics *syncx.RWSlice[string]
	mu                sync.RWMutex
	onTopicEmpty      func(topic string) // callback to unsubscribe gateway when topic has 0 peers
	forkManager       ForkManager
}

func NewPeerTopicTracer(log logger.AppLogger, s ForkManager, onTopicEmpty func(topic string)) *PeerTopicTracer {
	return &PeerTopicTracer{
		forkManager:       s,
		log:               log,
		discoveredTopics:  make(map[string]map[peer.ID]struct{}),
		unsubscribeTopics: syncx.NewRWSlice[string](),
		onTopicEmpty:      onTopicEmpty,
	}
}

// Trace implements the EventTracer interface
func (t *PeerTopicTracer) Trace(evt *pubsubpb.TraceEvent) {
	if evt == nil {
		return
	}

	// Remote subscription RPCs are the source of truth for discovered topics.
	// JOIN/LEAVE describe the local node, while GRAFT/PRUNE describe mesh membership.
	switch evt.GetType() {
	case pubsubpb.TraceEvent_RECV_RPC: // Handle multiple topics in RPC
		recvRPC := evt.GetRecvRPC()
		if recvRPC == nil {
			return
		}
		meta := recvRPC.GetMeta()
		if meta == nil {
			t.log.Debug("received RPC with no metadata")
			return
		}
		peerID := peer.ID(recvRPC.GetReceivedFrom())
		if peerID == "" {
			return
		}
		for _, subMsg := range meta.GetSubscription() {
			if subMsg == nil || subMsg.Topic == nil {
				continue
			}
			if subMsg.GetSubscribe() {
				t.addPeerTopic(peerID, *subMsg.Topic)
			} else {
				t.removePeerTopic(peerID, *subMsg.Topic)
			}
		}
		for _, m := range meta.GetMessages() {
			if m == nil || m.Topic == nil {
				continue
			}
			telemetry.LibP2PGossipsubRPCMessagesInc(*m.Topic, peerID.String(), "recv")
		}

	case pubsubpb.TraceEvent_SEND_RPC:
		sendRPC := evt.GetSendRPC()
		if sendRPC == nil {
			return
		}
		meta := sendRPC.GetMeta()
		if meta == nil {
			t.log.Debug("sent RPC with no metadata")
			return
		}
		var sendToPeer peer.ID
		if st := sendRPC.GetSendTo(); len(st) > 0 {
			sendToPeer = peer.ID(st)
		}
		if sendToPeer == "" {
			return
		}
		for _, m := range meta.GetMessages() {
			if m == nil || m.Topic == nil {
				continue
			}
			telemetry.LibP2PGossipsubRPCMessagesInc(*m.Topic, sendToPeer.String(), "send")
		}
	}
}

func (t *PeerTopicTracer) addPeerTopic(peerID peer.ID, topic string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.discoveredTopics[topic] == nil {
		t.discoveredTopics[topic] = make(map[peer.ID]struct{})
	}
	if _, exists := t.discoveredTopics[topic][peerID]; exists {
		// Already recorded
		return
	}
	t.discoveredTopics[topic][peerID] = struct{}{}

	// Log if it's an eth topic (we only need fork_digest for logging)
	if forkDigest := topics.GetForkDigestFromTopic(topic); forkDigest != "" {
		l := t.log.With(
			logger.WithPeerID(peerID),
			logger.WithTopic(topic),
			logger.WithString("fork_digest", forkDigest),
		)
		if t.forkManager.CheckForkSupported(forkDigest) {
			l.Debug("remote peer subscribed to new topic with supported fork digest")
			t.forkManager.SetObservedDigest(forkDigest)
		} else {
			l.Error("remote peer subscribed to non-supported fork digest", fmt.Errorf("non-supported fork digest"))
		}
	}
}

func (t *PeerTopicTracer) removePeerTopic(peerID peer.ID, topic string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.removePeerTopicLocked(peerID, topic)
}

func (t *PeerTopicTracer) removePeerTopicLocked(peerID peer.ID, topic string) {
	if peers, ok := t.discoveredTopics[topic]; ok {
		delete(peers, peerID)
		if len(peers) == 0 {
			delete(t.discoveredTopics, topic)
			// If topic has 0 peers, unsubscribe gateway from this topic
			if t.onTopicEmpty != nil {
				t.onTopicEmpty(topic)
			}
			t.unsubscribeTopics.Add(topic)
		}
	}
}

// RemovePeer removes a disconnected peer from every topic it advertised.
func (t *PeerTopicTracer) RemovePeer(peerID peer.ID) {
	if peerID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	for topic := range t.discoveredTopics {
		t.removePeerTopicLocked(peerID, topic)
	}
}

// GetDiscoveredTopics get all detected topics
func (t *PeerTopicTracer) GetDiscoveredTopics() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return commonmaps.MapKeys(t.discoveredTopics)
}

func (t *PeerTopicTracer) GetAndEraseUnsubscribeTopics() []string {
	return t.unsubscribeTopics.LoadAndErase()
}
