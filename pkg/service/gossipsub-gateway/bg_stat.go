package gossipsub_gateway

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func (s *Service) GetLibP2PPeers() (totalPeers int, perTopicPeers map[string]int, allPeerIDs []string, perTopicPeerIDs map[string][]string) {
	if s.ps == nil {
		return 0, nil, nil, nil
	}
	topics := s.ps.GetTopics()
	perTopicPeers = make(map[string]int, len(topics))
	perTopicPeerIDs = make(map[string][]string, len(topics))
	for _, t := range topics {
		peerList := s.ps.ListPeers(t)
		perTopicPeers[t] = len(peerList)
		ids := make([]string, len(peerList))
		for i, p := range peerList {
			ids[i] = p.String()
		}
		perTopicPeerIDs[t] = ids
	}
	peers := s.hostLibP2P.Network().Peers()
	allPeerIDs = make([]string, len(peers))
	for i, p := range peers {
		allPeerIDs[i] = p.String()
	}
	return len(peers), perTopicPeers, allPeerIDs, perTopicPeerIDs
}

func (s *Service) GetDirectLibP2PPeers() map[string]peer.AddrInfo {
	return s.libP2PDirectPeers.LoadAll()
}

func (s *Service) GetMumP2PPeers() (totalPeers int, perTopicPeers map[string]int, allPeerIDs []string, perTopicPeerIDs map[string][]string) {
	if s.nodeMumP2P == nil {
		return 0, nil, nil, nil
	}
	peers := s.nodeMumP2P.GetPeers()
	allPeerIDs = make([]string, len(peers))
	for i, p := range peers {
		allPeerIDs[i] = p.String()
	}
	topics := s.nodeMumP2P.GetTopics()
	perTopicPeers = make(map[string]int, len(topics))
	perTopicPeerIDs = make(map[string][]string, len(topics))
	for _, t := range topics {
		meshPeers := s.nodeMumP2P.GetMeshPeers(t)
		perTopicPeers[t] = len(meshPeers)
		ids := make([]string, len(meshPeers))
		for i, p := range meshPeers {
			ids[i] = p.String()
		}
		perTopicPeerIDs[t] = ids
	}
	return len(peers), perTopicPeers, allPeerIDs, perTopicPeerIDs
}

func (s *Service) dumpServiceStat() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		telemetry.SetPropagationState(s.cfg.PropagationEnabled())
		totalLibP2PPeers, perLibP2PTopic, _, libP2PTopicPeerIDs := s.GetLibP2PPeers()
		telemetry.ResetCLPeersPerTopic()
		for topic, peers := range perLibP2PTopic {
			telemetry.SetCLPeersPerTopic(topic, peers)
			if peers > 0 {
				s.log.Info("cl topic peers", logger.WithString("topic", topic), logger.WithInt("peers", peers))
			}
		}
		telemetry.SetCLMeshPeers(libP2PTopicPeerIDs)

		var totalMumP2PPeers int
		var perMumP2PTopic map[string]int
		_, _, _, mumP2PTopicPeerIDs := s.GetMumP2PPeers()
		telemetry.ResetMumP2PPeersPerTopic()
		if s.nodeMumP2P != nil {
			totalMumP2PPeers, perMumP2PTopic = s.nodeMumP2P.CountConnectedPeers()
			for topic, peers := range perMumP2PTopic {
				telemetry.SetMumP2PPeersPerTopic(topic, peers)
				if peers > 0 {
					s.log.Info("mump2p topic peers", logger.WithString("topic", topic), logger.WithInt("peers", peers))
				}
			}
		}
		telemetry.SetMumP2PMeshPeers(mumP2PTopicPeerIDs)
		telemetry.SetCLPeers(totalLibP2PPeers)
		telemetry.SetMumP2PPeers(totalMumP2PPeers)

		// Drain the per-topic send counters, Sum totals so the single summary line still.
		var sentMumP2P, sentLibP2P int
		for _, n := range s.statSendMum.LoadAllAndErase() {
			sentMumP2P += n
		}
		for _, n := range s.statSendLib.LoadAllAndErase() {
			sentLibP2P += n
		}

		s.log.Info("bg_stat",
			logger.WithInt("peers_libp2p", totalLibP2PPeers),
			logger.WithInt("peers_mump2p", totalMumP2PPeers),
			logger.WithInt("topics_libp2p", len(perLibP2PTopic)),
			logger.WithInt("topics_mump2p", len(perMumP2PTopic)),
			logger.WithInt("sent_libp2p", sentLibP2P),
			logger.WithInt("sent_mump2p", sentMumP2P),
			logger.WithUint64("bad_msgs_libp2p", telemetry.GetBadMessagesToCL()),
			logger.WithUint64("bad_msgs_mump2p", telemetry.GetBadMessagesToMum()),
		)
		for topic, badMessages := range telemetry.GetCLSSZErrors() {
			s.log.Info("failed ssz decode from CL", logger.WithTopic(topic), logger.WithUint64("count", badMessages))
		}
		for topic, badMessages := range telemetry.GetMumSSZErrors() {
			s.log.Info("failed ssz decode from MumP2P", logger.WithTopic(topic), logger.WithUint64("count", badMessages))
		}

		<-ticker.C
	}
}

// refreshForkDigestFromPeers periodically refreshes fork digest from discovered peer topics to detect fork migration.
// This runs with the same cadence as CL re-syncs topics (every epoch).
func (s *Service) refreshForkDigestFromPeers() {
	// Check every epoch (32 slots * 12 seconds = ~6.4 minutes)
	// Using 7 minutes to be safe
	ticker := time.NewTicker(7 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if s.peerTopicTracer == nil {
				s.log.Debug("peer topic tracer not initialized yet, skipping refresh")
				continue
			}
			s.refreshTopics()
		}
	}
}

func (s *Service) refreshTopics() {
	for _, topicUnsubscribe := range s.peerTopicTracer.GetAndEraseUnsubscribeTopics() {
		s.log.Info("cleaning up abandoned topic (0 CL peers)", logger.WithTopic(topicUnsubscribe))
		s.unsubscribeFromTopic(topicUnsubscribe)
	}

	for _, topicSubscribe := range s.peerTopicTracer.GetDiscoveredTopics() {
		l := s.log.With(logger.WithTopic(topicSubscribe))
		if utils.IsBeaconBlockTopic(topicSubscribe) && s.nodeMumP2P != nil {
			// we subscribe mump2p just for 2 topics - beacon and aggregate
			if err := s.nodeMumP2P.SubscribeTopic(topicSubscribe); err != nil {
				l.Error("failed to subscribe local mump2p node to topic", err)
			}
		}
		if err := s.subscribeToTopic(topicSubscribe); err != nil {
			l.Error("failed to subscribe local libp2p node to topic", err)
		}
	}
}
