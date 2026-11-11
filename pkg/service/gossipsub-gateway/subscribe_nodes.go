package gossipsub_gateway

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	mumP2PAggregatedMessagesTopic = "mump2p_aggregated_messages"

	// Fulu CUSTODY_REQUIREMENT. Lighthouse disconnects a peer that advertises less.
	advertisedCustodyGroupCount = 4
	// The gateway serves no blocks or sidecars; a slot past any real one makes clients that honor
	// earliest_available_slot skip it for those requests. 2^53-1 stays exact in JavaScript clients.
	noDataEarliestAvailableSlot consensus.Slot = 1<<53 - 1
	// Bump with any change to the advertised metadata: peers that cached it under this persistent
	// identity compare it with our ping reply to decide whether to re-fetch. TestMetadataSeqHistory checks this.
	metadataSeqNumber = 1

	statusV1Protocol protocol.ID = "/eth2/beacon_chain/req/status/1/ssz_snappy"
	statusV2Protocol protocol.ID = "/eth2/beacon_chain/req/status/2/ssz_snappy"
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
		s.log.Debug("skipping subscription to topic, not a beacon block or attestation topic", logger.WithTopic(topic))
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
	s.refreshTopics()
}

func (s *Service) handleSubscription(ctx context.Context, topicName string, sub *libp2ppubsub.Subscription) {
	defer s.unsubscribeFromTopic(topicName)
	forkDigest := topics.GetForkDigestFromTopic(topicName)
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
			if s.srvForkMgr.ObserveMessageDigest(forkDigest) {
				s.log.Info("active fork digest updated from CL traffic",
					logger.WithString("fork_digest", forkDigest),
					logger.WithTopic(topicName),
					logger.WithPeerID(msg.ReceivedFrom),
				)
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

	s.hostLibP2P.SetStreamHandler(statusV1Protocol, s.serveHandshake)
	s.hostLibP2P.SetStreamHandler(statusV2Protocol, s.serveHandshake)

	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/metadata/1/ssz_snappy", s.serveMetadata)
	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/metadata/2/ssz_snappy", s.serveMetadata)
	s.hostLibP2P.SetStreamHandler("/eth2/beacon_chain/req/metadata/3/ssz_snappy", s.serveMetadata)
}

// serveHandshake handles the eth2 CL status req/resp handshake (status/1, status/2).
// Distinct from the mump2p auth Handshake in handshake.go.
func (s *Service) serveHandshake(stream network.Stream) {
	defer stream.Close()
	topicHandshake := stream.Protocol()
	l := s.log.With(
		logger.WithFlow("handshake"),
		logger.WithTopic(string(topicHandshake)),
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
	switch st := msg.(type) {
	case *consensus.StatusV2:
		s.networkStatus.Store(st)
	case *consensus.Status:
		s.networkStatus.Store(st.V2())
	}
	s.finishStream(l, stream, advertisedStatus(msg))

	go s.subscribeOnce()
}

func newStatusMessage(proto protocol.ID) consensus.StatusMessage {
	switch proto {
	case statusV2Protocol:
		return &consensus.StatusV2{}
	case statusV1Protocol:
		return &consensus.Status{}
	}
	return nil
}

// Only status v2 carries earliest_available_slot; a v1 reply stays a plain echo.
func advertisedStatus(status consensus.StatusMessage) consensus.StatusMessage {
	v2, ok := status.(*consensus.StatusV2)
	if !ok {
		return status
	}
	out := *v2
	out.EarliestAvailableSlot = noDataEarliestAvailableSlot
	return &out
}

// outboundStatus builds the status for a negotiated protocol from the latest peer status.
func outboundStatus(last *consensus.StatusV2, proto protocol.ID) consensus.StatusMessage {
	if proto == statusV1Protocol {
		return last.V1()
	}
	return advertisedStatus(last)
}

func newMetadataMessage(topicMetadata string) consensus.Marshaler {
	// attnets: 64-bit bitvector => 8 bytes, LSB-first within each byte
	attNets := [8]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	syncNetsNone := [1]byte{0x00}

	if strings.Contains(topicMetadata, "metadata/1") {
		return &consensus.MetaDataV0{
			Attnets:   attNets,
			SeqNumber: metadataSeqNumber,
		}
	} else if strings.Contains(topicMetadata, "metadata/2") {
		return &consensus.MetaDataV1{
			Attnets:   attNets,
			SeqNumber: metadataSeqNumber,
			Syncnets:  syncNetsNone,
		}
	} else if strings.Contains(topicMetadata, "metadata/3") {
		return &consensus.MetaDataV2{
			SeqNumber:         metadataSeqNumber,
			Attnets:           attNets,
			Syncnets:          syncNetsNone,
			CustodyGroupCount: advertisedCustodyGroupCount,
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
		peerID := stream.Conn().RemotePeer()
		l := s.log.With(
			logger.WithFlow("goodbye"),
			logger.WithPeerID(peerID),
		)

		// 1) Decode SSZ-snappy payload: a single uint64 reason
		var reason consensus.SSZUint64
		if err := s.sszEncoder.DecodeWithMaxLength(stream, &reason); err != nil {
			l.Error("failed to decode goodbye", err)
			_ = stream.Reset()
			return
		}

		code := uint64(reason)
		reasonText, known := consensus.GoodbyeCodeMessages[code]
		reasonLabel := strconv.FormatUint(code, 10)
		if !known {
			reasonText, reasonLabel = "other", "other"
		}
		l.Info("received goodbye reason", logger.WithString("reason", reasonText), logger.WithUint64("code", code))
		if !s.isAllowedCLPeer(peerID) {
			return
		}
		telemetry.CLPeerGoodbye(s.peerClientFamily(peerID), reasonLabel)
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
		// Decoding validates the request; the peer's seq itself is unused because the reply carries ours.
		var peerSeq consensus.SSZUint64
		if err := s.sszEncoder.DecodeWithMaxLength(stream, &peerSeq); err != nil {
			// After a reset no one is left to answer; a half-close (io.EOF) still expects a reply.
			if errors.Is(err, network.ErrReset) {
				return
			}
			l.Error("failed to decode ping request", err)
			s.terminateStream(l, stream, "invalid ping request")
			return
		}
		pong := consensus.SSZUint64(metadataSeqNumber)
		s.finishStream(l, stream, &pong)
	})
}

func (s *Service) subscribeOnce() {
	s.oncer.Do(func() {
		s.log.Info("subscribing local node to topics after handshake")
		s.subscribeLocalNode()
	})
}

func (s *Service) finishStream(l logger.AppLogger, stream network.Stream, msg consensus.Marshaler) {
	s.writeResponse(l, stream, entities.ResponseCodeSuccess, msg)
}

// writeResponse resets on any write failure: a truncated chunk would read as corrupt data.
func (s *Service) writeResponse(l logger.AppLogger, stream network.Stream, code byte, msg consensus.Marshaler) bool {
	if _, err := stream.Write([]byte{code}); err != nil {
		l.Error("failed to send response code to peer", err)
		_ = stream.Reset()
		return false
	}
	if _, err := s.sszEncoder.EncodeWithMaxLength(stream, msg); err != nil {
		l.Error("failed to encode message to stream", err)
		_ = stream.Reset()
		return false
	}
	return true
}

// terminateStream answers InvalidRequest; the spec requires an ErrorMessage chunk after the code.
func (s *Service) terminateStream(l logger.AppLogger, stream network.Stream, reason string) {
	if s.writeResponse(l, stream, entities.ResponseCodeInvalidRequest, consensus.ErrorMessage(reason)) {
		_ = stream.Close()
	}
}

func (s *Service) rejectHandshakeForkMismatch(l logger.AppLogger, stream network.Stream, forkDigest string) {
	peerID := stream.Conn().RemotePeer()
	l.Info("handshake fork digest not supported, closing peer", logger.WithString("fork_digest", forkDigest))
	s.terminateStream(l, stream, "unsupported fork digest")
	if s.hostLibP2P != nil {
		if err := s.hostLibP2P.Network().ClosePeer(peerID); err != nil {
			l.Error("failed to close peer after fork mismatch", err, logger.WithPeerID(peerID))
		}
	}
}
