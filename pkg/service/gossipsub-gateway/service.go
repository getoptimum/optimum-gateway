package gossipsub_gateway

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/forks"
	"github.com/getoptimum/optimum-gateway/pkg/service/aggregator"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/bootstrapper"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
)

// networkBeaconStatus is the shared eth2 status + protocol used for all CL peers on one network.
type networkBeaconStatus struct {
	protocol string
	status   consensus.Marshaler
}

// Service orchestrates the gateway: it relays messages between the beacon-chain libp2p host and
// the mump2p node, manages topic subscriptions, and coordinates fork, auth, and bootstrap state.
type Service struct {
	ctx context.Context
	log logger.AppLogger
	cfg *config.AppConfig

	ps                     *libp2ppubsub.PubSub
	hostLibP2P             host.Host
	libP2PDirectPeers      *syncx.RWMap[string, peer.AddrInfo]
	directCLPeersAllowlist map[peer.ID]struct{} // allowlist of CL peers that can connect to the gateway host (libp2p)

	nodeMumP2P      mump2p.Engine
	nodeMumP2PBytes []byte
	nodeMumP2PStr   string

	oncer sync.Once // ensures that topic subscriptions are set up only once

	libP2PTopics *syncx.RWMap[string, *libp2ppubsub.Topic]
	libP2PSubs   *syncx.RWMap[string, *libp2ppubsub.Subscription]

	clMessages                  chan *entities.CLMessage        // messages from CL clients pass to mump2p nodes
	mumP2PMessages              chan *commonentities.P2PMessage // messages from mump2p nodes we're passing to CL client
	customMumP2PConnectionGater connmgr.ConnectionGater         // helper to reject connections from wrong networks, mostly used in tests
	mumP2PNodeOptions           []mump2p.NodeOption             // extra node options, used by tests to supply an in-process RLNC coder
	aggregatorMessages          *aggregator.Service
	srvMsgRouter                *message_router.Service
	authMgr                     *auth_token.Service // always non-nil; degrades to no-op when auth is disabled. Used for Bearer on outbound bootstrap calls and JWT in peer handshake.

	messagesMap   *syncx.TTLMap[uint64, struct{}]
	sszEncoder    *consensus.SSZSnappyCodec
	networkStatus atomic.Pointer[networkBeaconStatus]

	statSendMum *syncx.RWMap[string, int]
	statSendLib *syncx.RWMap[string, int]

	peerTopicTracer *tracer.PeerTopicTracer // tracer to discover topics from connected peers

	// srvForkMgr owns fork digest state and topic support checks.
	srvForkMgr      *forks.Service
	srvBootstrapper *bootstrapper.Service

	lastBlockReceivedAt atomic.Int64 // Unix ms — stamped on every beacon block from any source
	startedAt           time.Time    // wall-clock time the service was created
}

// LastBlockReceivedMs returns Unix ms of the last beacon block seen (0 if none).
func (s *Service) LastBlockReceivedMs() int64 {
	return s.lastBlockReceivedAt.Load()
}

// Option is a functional option applied to Service during construction.
type Option func(*Service)

// WithCustomMumP2PConnectionGater injects a custom mump2p connection gater; used primarily in tests
// to reject connections from the wrong network.
func WithCustomMumP2PConnectionGater(gater connmgr.ConnectionGater) func(*Service) {
	return func(s *Service) {
		s.customMumP2PConnectionGater = gater
	}
}

// WithMumP2PNodeOptions injects extra mump2p node options; used by tests to
// supply an in-process RLNC coder in place of the out-of-process one.
func WithMumP2PNodeOptions(opts ...mump2p.NodeOption) func(*Service) {
	return func(s *Service) {
		s.mumP2PNodeOptions = append(s.mumP2PNodeOptions, opts...)
	}
}

func NewService(
	ctx context.Context,
	log logger.AppLogger,
	cfg *config.AppConfig,
	srvMsgRouter *message_router.Service,
	authMgr *auth_token.Service,
	opts ...Option,
) (*Service, error) {
	srv := &Service{
		ctx:               ctx,
		cfg:               cfg,
		log:               log.With(logger.WithService("gateway")),
		libP2PTopics:      syncx.NewRWMap[string, *libp2ppubsub.Topic](),
		libP2PSubs:        syncx.NewRWMap[string, *libp2ppubsub.Subscription](),
		libP2PDirectPeers: syncx.NewRWMap[string, peer.AddrInfo](),
		clMessages:        make(chan *entities.CLMessage, 1_000),
		mumP2PMessages:    make(chan *commonentities.P2PMessage, 1_000),
		messagesMap:       syncx.NewTTLMap[uint64, struct{}](30*time.Second, 30*time.Second),
		sszEncoder:        &consensus.SSZSnappyCodec{},
		statSendMum:       syncx.NewRWMap[string, int](),
		statSendLib:       syncx.NewRWMap[string, int](),
		srvMsgRouter:      srvMsgRouter,
		authMgr:           authMgr,
	}
	srv.startedAt = time.Now()
	srv.aggregatorMessages = aggregator.NewAggregator(ctx, srv, cfg, log, srvMsgRouter.IsKnownValidator)
	srvForkMgr, err := forks.NewService(ctx, cfg, srv.log, srv.authMgr)
	if err != nil {
		return nil, fmt.Errorf("failed init fork service: %w", err)
	}
	srv.srvForkMgr = srvForkMgr
	srv.srvBootstrapper = bootstrapper.NewService(srv.ctx, srv.log, srv.cfg, srv.authMgr, srv.srvForkMgr)

	for _, opt := range opts {
		opt(srv)
	}
	go srv.srvForkMgr.Start(ctx)
	go srv.dumpServiceStat()
	go srv.dumpAggregateStats()
	go srv.refreshForkDigestFromPeers() // Periodically refresh fork digest from peer topics
	return srv, nil
}

func (s *Service) GetAuthManager() *auth_token.Service {
	return s.authMgr
}

func (s *Service) GetForkDigestManager() *forks.Service {
	return s.srvForkMgr
}

// GetMumP2PEngine returns the mump2p engine, or nil before the host is set up.
func (s *Service) GetMumP2PEngine() mump2p.Engine {
	return s.nodeMumP2P
}

// Run initializes the service by setting up the host and pubsub service.
// subscribes local node to the topics defined in the configuration,
// and establishes connections to mump2p nodes from the gateway hosts.
func (s *Service) Run() error {
	if err := s.setupLibP2PHost(); err != nil {
		return fmt.Errorf("unable to setup gateway host: %w", err)
	}
	// We use bootstrap node to get list of other gateways in network
	// each gateway subscribe to list of topics (defined in config) and we discover new gateways
	// once each gateway will warmed up, we expect that they create peer mesh to same active eth topics and exchange messages between them
	if err := s.setupMumP2PHost(); err != nil {
		return fmt.Errorf("failed setup mump2p host: %w", err)
	}
	go s.handleMessagesFromCL()         // handle messages from CL clients and pass them to mump2p nodes
	go s.handleMessagesFromMumP2PNode() // handle messages from LOCAL mump2p node
	return nil
}

// Stop gracefully stops the gateway service by closing the host.
func (s *Service) Stop() {
	s.log.Info("stopping gateway service")

	s.libP2PSubs.Range(func(_ string, sub *libp2ppubsub.Subscription) bool {
		sub.Cancel()
		return true
	})

	s.libP2PTopics.Range(func(_ string, tp *libp2ppubsub.Topic) bool {
		_ = tp.Close() // ignore error when service stopping
		return true
	})
	s.terminateLibP2PHost()
}

func (s *Service) terminateLibP2PHost() {
	s.log.Info("terminating libp2p host")
	if s.hostLibP2P != nil {
		if err := s.hostLibP2P.Close(); err != nil {
			s.log.Error("failed to close libp2p host", err)
		}
	}
	s.libP2PSubs.DeleteAll()
	s.libP2PTopics.DeleteAll()
	if s.nodeMumP2P != nil {
		s.nodeMumP2P.Stop()
	}
}

func (s *Service) GetHostInfo() peer.AddrInfo {
	return peer.AddrInfo{
		ID:    s.hostLibP2P.ID(),
		Addrs: s.hostLibP2P.Addrs(),
	}
}

func (s *Service) ConnectToPeer(ctx context.Context, peerAddr string) error {
	addrInfo, err := peer.AddrInfoFromString(peerAddr)
	if err != nil {
		return fmt.Errorf("failed to parse peer address %s: %w", peerAddr, err)
	}

	if err = s.hostLibP2P.Connect(ctx, *addrInfo); err != nil {
		return fmt.Errorf("failed to connect to peer %s: %w", addrInfo.ID, err)
	}
	s.log.Info("successfully connected to peer", logger.WithPeer(*addrInfo))
	return nil
}
