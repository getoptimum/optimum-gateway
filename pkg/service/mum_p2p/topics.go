package mum_p2p

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pubsub "github.com/libp2p/go-libp2p-pubsub"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// SubscribeTopic subscribes to a topic and calls the provided function when a message is received.
func (n *Node) SubscribeTopic(topicName string) error {
	if strings.TrimSpace(topicName) == "" {
		return fmt.Errorf("topic can't be empty")
	}
	if _, ok := n.topics.Load(topicName); ok {
		return nil
	}
	topic, err := n.ps.Join(topicName)
	if err != nil {
		return fmt.Errorf("failed to join GossipSub topic %s: %w", topicName, err)
	}
	n.topics.Store(topicName, topic)
	s, err := topic.Subscribe()
	if err != nil {
		n.topics.Delete(topicName)
		_ = topic.Close()
		return fmt.Errorf("failed to subscribe to GossipSub topic %s: %w", topicName, err)
	}
	n.subscriptions.Store(topicName, s)
	go n.handleSubscription(s, topicName)
	telemetry.SetP2PActiveTopics(n.topics.Len())
	n.tk.AddTopic(topicName)
	n.log.Info("subscribed to topic", logger.WithTopic(topicName))
	return nil
}

// handleSubscription processes incoming messages from the subscription.
// It runs in a separate goroutine and listens for messages until the context is done.
// It logs any errors encountered while receiving messages.
func (n *Node) handleSubscription(s *pubsub.Subscription, topicName string) {
	hID := n.GetHostInfo().ID.String()
	for {
		select {
		case <-n.ctx.Done():
			return
		default:
			msg, err := s.Next(n.ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				n.log.Error("failed to get message", err)
				continue
			}

			n.broadcaster.Broadcast(&entities.MumP2PResponse{
				Message: &commonentities.P2PMessage{
					MessageID:      msg.ID,
					UpstreamPeerID: hID,
					SourceNodeID:   msg.ReceivedFrom.String(),
					Topic:          topicName,
					Message:        msg.Data,
				},
				Command: entities.MumP2PCommandMessage,
			})
		}
	}
}

// UnsubscribeTopic unsubscribes from a topic and closes the associated subscription.
// It returns an error if the topic is not found or if closing the subscription fails.
func (n *Node) UnsubscribeTopic(topicName string) error {
	if sub, ok := n.subscriptions.Load(topicName); ok {
		sub.Cancel()
		n.subscriptions.Delete(topicName)
	}
	topic, ok := n.topics.Load(topicName)
	if !ok {
		return nil // topic not found, nothing to do, probably already unsubscribed
	}
	if err := topic.Close(); err != nil {
		return fmt.Errorf("failed to close topic %s: %w", topicName, err)
	}
	n.topics.Delete(topicName)
	telemetry.SetP2PActiveTopics(n.topics.Len())
	n.tk.RemoveTopic(topicName)
	n.log.Info("unsubscribed topic", logger.WithTopic(topicName))
	return nil
}

func (n *Node) reannounceTopics() {
	for _, topic := range append([]string(nil), n.GetTopics()...) {
		if err := n.UnsubscribeTopic(topic); err != nil {
			n.log.Error("failed to drop topic for mesh reannounce", err, logger.WithTopic(topic))
			continue
		}
		if err := n.SubscribeTopic(topic); err != nil {
			n.log.Error("failed to resubscribe topic for mesh reannounce", err, logger.WithTopic(topic))
		}
	}
}
