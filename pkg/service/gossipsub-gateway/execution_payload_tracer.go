package gossipsub_gateway

import (
	"strings"
	"time"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// executionPayloadTopicSuffix is checked instead of utils.IsExecutionPayloadTopic
// because the tracer sees every libp2p message, including all attestation subnets.
var executionPayloadTopicSuffix = "/" + utils.ExecutionPayloadBase + "/ssz_snappy"

// executionPayloadTracer stamps every remote libp2p copy of an envelope inside
// pubsub, before validation. Once the gateway has published the mesh copy to
// its CL, a later libp2p copy is a pubsub duplicate that never reaches
// handleSubscription; DuplicateMessage still sees it.
type executionPayloadTracer struct {
	log logger.AppLogger
}

var _ libp2ppubsub.RawTracer = (*executionPayloadTracer)(nil)

func (t *executionPayloadTracer) ValidateMessage(msg *libp2ppubsub.Message) {
	t.arrived(msg, true)
}

func (t *executionPayloadTracer) DuplicateMessage(msg *libp2ppubsub.Message) {
	t.arrived(msg, false)
}

func (t *executionPayloadTracer) arrived(msg *libp2ppubsub.Message, firstSeen bool) {
	topic := msg.GetTopic()
	if !strings.HasSuffix(topic, executionPayloadTopicSuffix) {
		return
	}
	logExecutionPayloadArrival(t.log, time.Now(), "cl", msg.GetData(),
		logger.WithString("source", entities.SourceLibP2P.String()),
		logger.WithTopic(topic),
		logger.WithPeerID(msg.ReceivedFrom),
		logger.WithBool("first_seen", firstSeen),
	)
}

func (t *executionPayloadTracer) AddPeer(peer.ID, protocol.ID)                {}
func (t *executionPayloadTracer) RemovePeer(peer.ID)                          {}
func (t *executionPayloadTracer) OnNewOutboundStream(peer.ID, protocol.ID)    {}
func (t *executionPayloadTracer) OnClosedOutboundStream(peer.ID)              {}
func (t *executionPayloadTracer) Join(string)                                 {}
func (t *executionPayloadTracer) Leave(string)                                {}
func (t *executionPayloadTracer) Graft(peer.ID, string)                       {}
func (t *executionPayloadTracer) Prune(peer.ID, string)                       {}
func (t *executionPayloadTracer) DeliverMessage(*libp2ppubsub.Message)        {}
func (t *executionPayloadTracer) RejectMessage(*libp2ppubsub.Message, string) {}
func (t *executionPayloadTracer) ThrottlePeer(peer.ID)                        {}
func (t *executionPayloadTracer) RecvRPC(*libp2ppubsub.RPC)                   {}
func (t *executionPayloadTracer) SendRPC(*libp2ppubsub.RPC, peer.ID)          {}
func (t *executionPayloadTracer) DropRPC(*libp2ppubsub.RPC, peer.ID)          {}
func (t *executionPayloadTracer) UndeliverableMessage(*libp2ppubsub.Message)  {}
