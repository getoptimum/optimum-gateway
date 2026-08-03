package gossipsub_gateway

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
)

// buildMumP2PConfig assembles the mump2p node config. The RLNC and mesh parameters
// come from the dynamic config rotator so operator changes reach the constructed node;
// the package defaults stand only until the rotator has a configuration to serve.
func buildMumP2PConfig(appCfg *config.AppConfig, bootstrapPeers []string) *mump2p.Config {
	rotator := appCfg.GetDCRotator()
	optCfg := &mump2p.Config{
		ListenPort:               appCfg.AgentMumP2PPort,
		MaxMessageSize:           config.DefaultMaxMessageSize,
		RandomMessageSize:        config.DefaultRandomMessageSize,
		ShardFactor:              int(config.DefaultShardFactor),
		PublisherShardMultiplier: config.DefaultPublisherShardMultiplier,
		ForwardShardThreshold:    config.DefaultForwardShardThreshold,
		MeshDegreeTarget:         int(config.DefaultMeshDegreeTarget),
		MeshDegreeMin:            int(config.DefaultMeshDegreeMin),
		MeshDegreeMax:            int(config.DefaultMeshDegreeMax),
		BootstrapPeers:           bootstrapPeers,
		ClusterID:                appCfg.GatewayClusterID,
		GatewayID:                appCfg.GatewayID,
		SHMName:                  appCfg.SHMName,
		SHMLanes:                 appCfg.SHMLanes,
		Rotator:                  rotator,
		TraceMesh:                appCfg.TraceMesh,
		TraceRPC:                 appCfg.TraceRPC,
		TraceShard:               appCfg.TraceShard,
		DatagramEnable:           appCfg.DatagramEnable,
		DatagramListenAddr:       appCfg.DatagramListenAddr,
		DatagramMaxPayload:       appCfg.DatagramMaxPayload,
		OTelEnable:               appCfg.OTelEnable,
		OTelEndpoint:             appCfg.OTelEndpoint,
		OTelInsecure:             appCfg.OTelInsecure,
		OTelSampleRatio:          appCfg.OTelSampleRatio,
	}
	if rotator == nil {
		return optCfg
	}
	served := rotator.Get()
	if served == nil {
		return optCfg
	}
	// Served values are copied wholesale, zeros included: 0 is a legitimate setting
	// for these knobs, so it must not be mistaken for "unset" and replaced by a default.
	optCfg.RandomMessageSize = served.RandomMessageSize
	optCfg.ShardFactor = int(served.ShardFactor)
	optCfg.PublisherShardMultiplier = served.PublisherShardMultiplier
	optCfg.ForwardShardThreshold = served.ForwardShardThreshold
	optCfg.MeshDegreeTarget = int(served.MeshDegreeTarget)
	optCfg.MeshDegreeMin = int(served.MeshDegreeMin)
	optCfg.MeshDegreeMax = int(served.MeshDegreeMax)
	return optCfg
}

func (s *Service) setupMumP2PHost() error {
	filtered, err := s.srvBootstrapper.RegisterAndGetMumP2PPeers()
	if err != nil {
		return fmt.Errorf("failed register mump2p peer: %w", err)
	}
	s.log.Info("setting mump2p host")
	optCfg := buildMumP2PConfig(s.cfg, filtered)
	if s.customMumP2PConnectionGater != nil {
		optCfg.CustomConnectionGater = s.customMumP2PConnectionGater
	}

	nodeOpts := append([]mump2p.NodeOption{
		mump2p.WithCustomHandshakeBuilder(s.handshakeBuilder),
		mump2p.WithCustomHandshakeHandler(s.handshakeHandler),
	}, s.mumP2PNodeOptions...)

	s.nodeMumP2P, err = mump2p.NewNode(
		s.ctx,
		s.log.With(logger.WithService("mump2p")),
		optCfg,
		s.cfg.IdentityMumP2PDir,
		nodeOpts...,
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
