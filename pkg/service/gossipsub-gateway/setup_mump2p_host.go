package gossipsub_gateway

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
)

func (s *Service) setupMumP2PHost() error {
	filtered, err := s.srvBootstrapper.RegisterAndGetMumP2PPeers()
	if err != nil {
		return fmt.Errorf("failed register mump2p peer: %w", err)
	}
	s.log.Info("setting mump2p host")
	optCfg := &mum_p2p.Config{
		ListenPort:               s.cfg.AgentMumP2PPort,
		MaxMessageSize:           config.DefaultMaxMessageSize,
		RandomMessageSize:        config.DefaultRandomMessageSize,
		ShardFactor:              config.DefaultShardFactor,
		PublisherShardMultiplier: config.DefaultPublisherShardMultiplier,
		ForwardShardThreshold:    config.DefaultForwardShardThreshold,
		MeshDegreeTarget:         int(config.DefaultMeshDegreeTarget),
		MeshDegreeMin:            int(config.DefaultMeshDegreeMin),
		MeshDegreeMax:            int(config.DefaultMeshDegreeMax),
		BootstrapPeers:           filtered,
		ClusterID:                s.cfg.GatewayClusterID,
		Rotator:                  s.cfg.GetDCRotator(),
		TraceMesh:                s.cfg.TraceMesh,
		TraceRPC:                 s.cfg.TraceRPC,
		TraceShard:               s.cfg.TraceShard,
	}
	if s.customMumP2PConnectionGater != nil {
		optCfg.CustomConnectionGater = s.customMumP2PConnectionGater
	}

	s.nodeMumP2P, err = mum_p2p.NewNode(
		s.ctx,
		s.log,
		optCfg,
		s.cfg.IdentityMumP2PDir,
		mum_p2p.WithCustomHandshakeBuilder(s.handshakeBuilder),
		mum_p2p.WithCustomHandshakeHandler(s.handshakeHandler),
	)
	if err != nil {
		return fmt.Errorf("failed to setup mump2p host: %w", err)
	}
	s.log.Info("establish connections to mump2p nodes", logger.WithInt("nodes_count", len(filtered)))
	if err = s.nodeMumP2P.Start(); err != nil {
		return fmt.Errorf("failed starting mump2p node: %w", err)
	}
	s.nodeMumP2PStr = s.nodeMumP2P.GetHostInfo().ID.String()
	s.srvBootstrapper.SetGatewayPeerIDStr(s.nodeMumP2PStr)
	s.nodeMumP2PBytes, err = s.nodeMumP2P.GetHost().ID().Marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal mump2p mesh host ID: %w", err)
	}
	filteredTopics := s.filterAndBuildEthTopics(s.srvForkMgr.ActiveDigest(), true)
	for _, targetTopic := range filteredTopics {
		if err = s.nodeMumP2P.SubscribeTopic(targetTopic); err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", targetTopic, err)
		}
	}
	go s.handleMessagesFromMumP2PTopics()
	s.log.Info("setup mump2p host completed")
	s.nodeMumP2P.GetHost().Network().Notify(
		&network.NotifyBundle{
			ConnectedF: func(_ network.Network, _ network.Conn) {
				telemetry.MumP2PPeerConnected()
			},
			DisconnectedF: func(_ network.Network, _ network.Conn) {
				telemetry.MumP2PPeerDisconnected()
			},
		},
	)
	return nil
}

func (s *Service) handleMessagesFromMumP2PTopics() {
	key := uuid.NewString()
	dataChan := s.nodeMumP2P.RegisterListener(key)
	defer s.nodeMumP2P.UnregisterListener(key)
	for {
		select {
		case <-s.ctx.Done():
			return
		case data, ok := <-dataChan:
			if !ok {
				return
			}
			switch data.Command {
			case entities.MumP2PCommandMessage:
				select {
				case s.mumP2PMessages <- data.Message:
				case <-s.ctx.Done():
					return
				}
			case entities.MumP2PCommandTraceMumP2P:
				if err := tracer.HandleMumP2PTrace(data.TraceEvent); err != nil {
					s.log.Error("failed to handle mump2p trace message", err)
				}
			default:
			}
		}
	}
}
