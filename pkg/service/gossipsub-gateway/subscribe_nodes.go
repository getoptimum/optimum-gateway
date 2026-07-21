package gossipsub_gateway

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	mumP2PAggregatedMessagesTopic = "mump2p_aggregated_messages"
)

// filterAndBuildEthTopics returns full eth2 topics built from descriptors (using forkDigest) and, for mump2p nodes, also appends the internal aggregated-messages topic.
func (s *Service) filterAndBuildEthTopics(forkDigest string, mumP2PNode bool) []string {
	if mumP2PNode {
		return []string{
			s.buildFullTopic(forkDigest, utils.BeaconBlockBase),
			mumP2PAggregatedMessagesTopic,
		}
	}

	descriptors := utils.UnpackTopics([]string{
		utils.BeaconAttestationBase,
		utils.BeaconBlockBase,
	})
	filtered := make([]string, 0, len(descriptors))
	for _, topic := range descriptors {
		if full := s.buildFullTopic(forkDigest, topic); full != "" {
			filtered = append(filtered, full)
		}
	}
	return filtered
}

// buildFullTopic returns full eth2 topic or as-is if already full.
func (s *Service) buildFullTopic(forkDigest, topic string) string {
	topic = strings.TrimSpace(topic)
	if topics.IsFullEth2Topic(topic) {
		return topic
	}
	if forkDigest == "" {
		s.log.Debug("skipping topic, fork digest not yet available", logger.WithTopic(topic))
		return ""
	}
	return topics.BuildFullTopic(forkDigest, topic)
}

// subscribeToTopic subscribes to a single topic.
func (s *Service) subscribeToTopic(topic string) error {
	valid := utils.IsBeaconBlockTopic(topic) || utils.IsAttestationTopic(topic) // validate topic format
	if !valid {
		s.log.Info("skipping subscription to topic, not a beacon block or attestation topic", logger.WithTopic(topic))
		return nil
	}
	// Check if already subscribed
	if _, ok := s.libP2PSubs.Load(topic); ok {
		return nil
	}

	if s.ps == nil {
		return fmt.Errorf("pubsub service not initialized")
	}
	s.log.Info("subscribing to topic", logger.WithTopic(topic))
	topicHandler, err := s.ps.Join(topic)
	if err != nil {
		return fmt.Errorf("failed to join topic %s: %w", topic, err)
	}

	s.libP2PTopics.Store(topic, topicHandler)
	sub, err := topicHandler.Subscribe()
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
	}

	s.libP2PSubs.Store(topic, sub)

	go s.handleSubscription(s.ctx, topic, sub)
	return nil
}

func (s *Service) subscribeLocalNode() {
	s.log.Info("subscribing to local libp2p node")
	filteredTopics := s.filterAndBuildEthTopics(s.srvForkMgr.ActiveDigest(), false)
	for _, targetTopic := range filteredTopics {
		if err := s.subscribeToTopic(targetTopic); err != nil {
			s.log.Error("failed to subscribe to topic", err, logger.WithTopic(targetTopic))
			continue
		}
	}
	s.refreshTopics() // refresh topics to catch any new discovered topics from previous runs
}

func (s *Service) handleSubscription(ctx context.Context, topicName string, sub *libp2ppubsub.Subscription) {
	defer s.unsubscribeFromTopic(topicName)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := sub.Next(ctx)
			if err != nil {
				if errors.Is(err, libp2ppubsub.ErrSubscriptionCancelled) ||
					errors.Is(err, context.Canceled) ||
					errors.Is(err, context.DeadlineExceeded) {
					s.log.Info("subscription canceled", logger.WithTopic(topicName))
					return
				}
				s.log.Error("failed read message", err)
				continue
			}

			if msg.ReceivedFrom == s.hostLibP2P.ID() {
				continue
			}

			select {
			case s.clMessages <- &entities.CLMessage{
				MessageID:    msg.ID,
				Topic:        topicName,
				Message:      msg.Data,
				ReceivedFrom: msg.ReceivedFrom.String(),
			}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *Service) unsubscribeFromTopic(topic string) {
	// Unsubscribe from CL (libp2p) topic
	if sub, ok := s.libP2PSubs.Load(topic); ok {
		sub.Cancel()
		s.libP2PSubs.Delete(topic)
	}
	l := s.log.With(logger.WithTopic(topic))
	if t, ok := s.libP2PTopics.Load(topic); ok {
		if err := t.Close(); err != nil && !strings.Contains(err.Error(), "context canceled") {
			l.Error("failed to close topic", err)
		}
		// Always delete the topic entry after close attempt - a failed close
		// leaves the handle in an undefined state that shouldn't be reused
		s.libP2PTopics.Delete(topic)
	}
	l.Info("unsubscribed from CL topic")

	// Unsubscribe from mump2p topic (always attempt, even if CL unsubscribe had errors)
	if s.nodeMumP2P != nil {
		if err := s.nodeMumP2P.UnsubscribeTopic(topic); err != nil && !strings.Contains(err.Error(), "context canceled") {
			l.Error("failed to unsubscribe from mump2p topic", err)
		} else {
			l.Info("unsubscribed from mump2p topic")
		}
	}
}

func (s *Service) handleHandshake() {
	s.registerGoodByeHandler("/eth2/beacon_chain/req/goodbye/1/ssz_snappy")
	s.registerPingHandler("/eth2/beacon_chain/req/ping/1/ssz_snappy")

	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/status/1/ssz_snappy", s.serveHandshake)
	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/status/2/ssz_snappy", s.serveHandshake)

	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/metadata/1/ssz_snappy", s.serveMetadata)
	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/metadata/2/ssz_snappy", s.serveMetadata)
	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/metadata/3/ssz_snappy", s.serveMetadata)
}

// serveHandshake handles the eth2 CL status req/resp handshake (status/1, status/2).
// Distinct from the mump2p auth Handshake in handshake.go.
func (s *Service) serveHandshake(stream network.Stream) {
	defer stream.Close()
	topicHandshake := string(stream.Protocol())
	l := s.log.With(
		logger.WithFlow("handshake"),
		logger.WithTopic(topicHandshake),
		logger.WithPeerID(stream.Conn().RemotePeer()),
	)
	l.Debug("received handshake message stream")

	msg := newStatusMessage(topicHandshake)
	if msg == nil {
		l.Error("unsupported status protocol", fmt.Errorf("unsupported topic"))
		return
	}
	if err := s.sszEncoder.DecodeWithMaxLength(stream, msg); err != nil {
		l.Error("failed to decode status request", err)
		return
	}
	digest := msg.GetForkDigest()
	forkDigest := hex.EncodeToString(digest[:])
	if !s.srvForkMgr.CheckForkSupported(forkDigest) {
		s.rejectHandshakeForkMismatch(l, stream, forkDigest)
		return
	}
	s.setNetworkBeaconStatus(topicHandshake, msg)
	s.finishStream(l, stream, msg)

	go s.subscribeOnce()
}

func newStatusMessage(topicHandshake string) consensus.StatusMessage {
	if strings.Contains(topicHandshake, "req/status/2") {
		return &consensus.StatusV2{}
	} else if strings.Contains(topicHandshake, "req/status/1") {
		return &consensus.Status{}
	}
	return nil
}

func newMetadataMessage(topicMetadata string) consensus.Marshaler {
	// attnets: 64-bit bitvector => 8 bytes, LSB-first within each byte
	attNets := [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	syncNetsNone := [1]byte{0x00}

	if strings.Contains(topicMetadata, "metadata/1") {
		return &consensus.MetaDataV0{
			Attnets:   attNets,
			SeqNumber: 0, // initial; increase per request if ordering needed
		}
	} else if strings.Contains(topicMetadata, "metadata/2") {
		return &consensus.MetaDataV1{
			Attnets:   attNets,
			SeqNumber: 0, // initial; increase per request if ordering needed
			Syncnets:  syncNetsNone,
		}
	} else if strings.Contains(topicMetadata, "metadata/3") {
		return &consensus.MetaDataV2{
			Attnets:           attNets,
			Syncnets:          syncNetsNone,
			CustodyGroupCount: 8, // Fulu VALIDATOR_CUSTODY_REQUIREMENT; valid range [4, 128]
		}
	}
	return nil
}

func (s *Service) serveMetadata(stream network.Stream) {
	defer stream.Close()
	topicMetadata := string(stream.Protocol())
	l := s.log.With(
		logger.WithFlow("metadata"),
		logger.WithTopic(topicMetadata),
		logger.WithPeerID(stream.Conn().RemotePeer()),
	)
	l.Debug("received metadata request")
	s.finishStream(l, stream, newMetadataMessage(topicMetadata))
	_, _ = stream.Read([]byte{0})
}

func (s *Service) registerGoodByeHandler(topicGoodbye string) {
	s.hostLibP2P.SetStreamHandler(protocol.ID(topicGoodbye), func(stream network.Stream) {
		defer stream.Close()
		l := s.log.With(
			logger.WithFlow("goodbye"),
			logger.WithString("peer", stream.Conn().RemotePeer().String()),
		)

		// 1) Decode SSZ-snappy payload: a single uint64 reason
		var reason consensus.SSZUint64
		if err := s.sszEncoder.DecodeWithMaxLength(stream, &reason); err != nil {
			l.Error("failed to decode goodbye", err)
			_ = stream.Reset()
			return
		}

		reasonText := consensus.GoodbyeCodeMessages[uint64(reason)]
		l.Info("received goodbye reason", logger.WithString("reason", reasonText))
	})
}

func (s *Service) registerPingHandler(topicPing string) {
	s.hostLibP2P.SetStreamHandler(protocol.ID(topicPing), func(stream network.Stream) {
		defer stream.Close()
		l := s.log.With(
			logger.WithFlow("ping"),
			logger.WithPeerID(stream.Conn().RemotePeer()),
		)
		l.Debug("received ping message")
		msg := new(consensus.SSZUint64)
		if err := s.sszEncoder.DecodeWithMaxLength(stream, msg); err != nil {
			l.Error("failed to decode ping request", err)
			return
		}
		s.finishStream(l, stream, msg)
	})
}

func (s *Service) subscribeOnce() {
	s.oncer.Do(func() {
		s.log.Info("subscribing local node to topics after handshake")
		s.subscribeLocalNode()
	})
}

func (s *Service) setNetworkBeaconStatus(statusProtocol string, status consensus.Marshaler) {
	s.networkStatus.Store(&networkBeaconStatus{
		protocol: statusProtocol,
		status:   status,
	})
}

func (s *Service) finishStream(l logger.AppLogger, stream network.Stream, msg consensus.Marshaler) {
	if _, err := stream.Write([]byte{entities.ResponseCodeSuccess}); err != nil {
		l.Error("failed to send response code to peer", err)
		return
	}
	if _, err := s.sszEncoder.EncodeWithMaxLength(stream, msg); err != nil {
		l.Error("failed to encode message to stream", err)
		return
	}
}

func (s *Service) terminateStream(l logger.AppLogger, stream network.Stream) {
	if _, err := stream.Write([]byte{entities.ResponseCodeInvalidRequest}); err != nil {
		l.Error("failed to send error response code to peer", err)
	}
	_ = stream.Close()
}

func (s *Service) rejectHandshakeForkMismatch(l logger.AppLogger, stream network.Stream, forkDigest string) {
	peerID := stream.Conn().RemotePeer()
	l.Info("handshake fork digest not supported, closing peer", logger.WithString("fork_digest", forkDigest))
	s.terminateStream(l, stream)
	if s.hostLibP2P != nil {
		if err := s.hostLibP2P.Network().ClosePeer(peerID); err != nil {
			l.Error("failed to close peer after fork mismatch", err, logger.WithPeerID(peerID))
		}
	}
}
