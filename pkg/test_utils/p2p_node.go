package test_utils

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p"
	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	node "github.com/getoptimum/optimum-common/pkg/identity"
)

type P2PNode struct {
	Host        host.Host
	PubSub      *libp2ppubsub.PubSub
	hostAddress string

	topic map[string]*libp2ppubsub.Topic
	sub   map[string]*libp2ppubsub.Subscription
}

// SpawnLocalCLLibP2PNode starts a slim libp2p CL peer for tests.
func SpawnLocalCLLibP2PNode(ctx context.Context, t *testing.T, remotePeer peer.AddrInfo, port int, identityDir string) *P2PNode {
	t.Helper()

	key, err := node.EnsureIdentity(identityDir)
	require.NoError(t, err)
	listenAddr := fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port)
	libP2POpts := []libp2p.Option{
		libp2p.ListenAddrStrings(listenAddr),
		libp2p.Identity(key),
	}

	h, err := libp2p.New(libP2POpts...)
	require.NoError(t, err, "failed to create libp2p host")
	options := []libp2ppubsub.Option{
		libp2ppubsub.WithMessageSignaturePolicy(libp2ppubsub.StrictNoSign),
		libp2ppubsub.WithNoAuthor(),
		libp2ppubsub.WithMessageIdFn(func(m *pubsubpb.Message) string {
			return commonhash.SHA256(m.Data)
		}),
		libp2ppubsub.WithPeerExchange(true),
		libp2ppubsub.WithMaxMessageSize(1024 * 1024),
		libp2ppubsub.WithDirectPeers([]peer.AddrInfo{remotePeer}),
	}
	ps, err := libp2ppubsub.NewGossipSub(ctx, h, options...)
	require.NoError(t, err, "failed to create pubsub instance")
	return &P2PNode{
		Host:        h,
		PubSub:      ps,
		hostAddress: listenAddr,

		topic: make(map[string]*libp2ppubsub.Topic),
		sub:   make(map[string]*libp2ppubsub.Subscription),
	}
}

func (p *P2PNode) GetHostAddress() string {
	return fmt.Sprintf("%s/p2p/%s", p.hostAddress, p.Host.ID().String())
}

func (p *P2PNode) SubscribeToTopic(ctx context.Context, t *testing.T, topicNames []string, handler func(*libp2ppubsub.Message)) {
	t.Helper()

	var err error
	for _, topicName := range topicNames {
		p.topic[topicName], err = p.PubSub.Join(topicName)
		require.NoError(t, err)

		p.sub[topicName], err = p.topic[topicName].Subscribe()
		require.NoError(t, err)

		t.Cleanup(func() {
			_ = p.topic[topicName].Close()
			p.sub[topicName].Cancel()
		})
	}
	// start listening
	for _, topicName := range topicNames {
		go p.handleSubscription(ctx, topicName, handler)
	}
}

func (p *P2PNode) handleSubscription(ctx context.Context, topicName string, handler func(*libp2ppubsub.Message)) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := p.sub[topicName].Next(ctx)
			if err != nil {
				if strings.Contains(err.Error(), "context canceled") {
					return
				}
				continue
			}
			handler(msg)
		}
	}
}

func (p *P2PNode) PublishMessage(ctx context.Context, topicName string, msg []byte) error {
	return p.topic[topicName].Publish(ctx, msg)
}
