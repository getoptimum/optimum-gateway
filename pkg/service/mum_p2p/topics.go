package mum_p2p

import (
	"fmt"
	"strings"

	rlncps "github.com/getoptimum/mump2p-protocol/pkg/pubsub"
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
	topic, err := rlncps.JoinPartialTopic(n.ps, topicName)
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
	telemetry.SetP2PActiveTopics(n.topics.Len())
	n.tk.AddTopic(topicName)
	n.log.Info("subscribed to topic", logger.WithTopic(topicName))
	return nil
}

// runPartialDeliveries reads reconstructed payloads from the partial manager (Deliveries).
// topic.Subscribe() is only for mesh membership; do not use subscription.Next() for app data.
func (n *Node) runPartialDeliveries() {
	hID := n.GetHostInfo().ID.String()
	for {
		select {
		case <-n.ctx.Done():
			return
		case delivery := <-n.psRouter.Deliveries():
			if _, ok := n.topics.Load(delivery.Topic); !ok {
				continue
			}
			n.broadcaster.Broadcast(&entities.MumP2PResponse{
				Message: &commonentities.P2PMessage{
					MessageID:      delivery.GroupID,
					UpstreamPeerID: hID,
					SourceNodeID:   delivery.From.String(),
					Topic:          delivery.Topic,
					Message:        delivery.Payload,
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
