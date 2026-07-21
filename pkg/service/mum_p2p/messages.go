package mum_p2p

import (
	"context"
	"fmt"
)

// PublishMessage publishes a message to the specified topic.
// If the topic is not already subscribed, it will attempt to subscribe first.
// It returns an error if the topic is not found or if publishing the message fails.
func (n *Node) PublishMessage(
	ctx context.Context,
	topicName string,
	msg []byte,
) error {
	topic, ok := n.topics.Load(topicName)
	if !ok {
		if err := n.SubscribeTopic(topicName); err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topicName, err)
		}
		topic, ok = n.topics.Load(topicName)
		if !ok {
			return fmt.Errorf("topic %s not found after subscribing", topicName)
		}
	}
	if err := topic.Publish(ctx, msg); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}
	return nil
}
