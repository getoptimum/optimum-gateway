package mump2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	"github.com/multiformats/go-multiaddr"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	rlncps "github.com/getoptimum/mump2p-protocol/pkg/pubsub"
	rlncrouter "github.com/getoptimum/mump2p-protocol/pkg/router"
	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	discovery "github.com/getoptimum/optimum-gateway/pkg/service/mump2p/dhtdiscovery"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p/topics_keeper"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p/tracer"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

type Node struct {
	ctx  context.Context
	log  logger.AppLogger
	cfg  *Config
	host host.Host      // The libp2p host managing network connections and identity
	ps   *pubsub.PubSub // The mump2p RLNC pub-sub instance

	router *rlncrouter.RLNCRouter // owns the RLNC decode state; closed on Stop
	dgram  *rlncps.Datagram       // nil unless the datagram data plane is enabled

	bootstrapNodes []peer.AddrInfo                            // Optional bootstrap peers for initial connectivity
	topics         *syncx.RWMap[string, *pubsub.Topic]        // Active topics
	subscriptions  *syncx.RWMap[string, *pubsub.Subscription] // Active topic subscriptions
	broadcaster    *syncx.Broadcaster[*entities.MumP2PResponse]

	peersMap *syncx.TTLMap[peer.ID, entities.PeerState]

	// peersApprovedMap is the mesh allow set: default-deny admission consults it
	// on every staging, send-target and receive decision. It is connection
	// scoped, cleared on disconnect, and deliberately not TTL bounded like
	// peersMap: an expiring entry would silently partition a live peer.
	peersApprovedMap *syncx.RWMap[peer.ID, struct{}]

	tk *topics_keeper.Service // Topics keeper for persisting subscribed topics. Using on node startup.

	handshakeBuilder func() any                                        // function that create handshake message
	handshakeHandler func(peerID peer.ID, decoder *json.Decoder) error // function that parse and validate handshake message

	oncer sync.Once
}

// NewNode creates a new P2P node instance using the provided config.
// It sets up the libp2p host with a listen address and initializes mump2p pub-sub.
func NewNode(
	ctx context.Context,
	log logger.AppLogger,
	cfg *Config,
	identityDir string,
	opts ...NodeOption,
) (*Node, error) {
	key, err := identity.EnsureIdentity(identityDir)
	if err != nil {
		return nil, fmt.Errorf("failed ensuring identity from %s: %w", identityDir, err)
	}

	publicIPV4, publicIPV6, err := commonnet.GetExternalIPs()
	if err != nil {
		return nil, fmt.Errorf("failed to get public IP address: %w", err)
	}
	log.Info("ip detected", logger.WithString("ipv4", publicIPV4), logger.WithString("ipv6", publicIPV6))

	identityKey, err := identity.ExtractIdentityFromDir(identityDir)
	if err != nil {
		return nil, fmt.Errorf("unable to extract identity from %s: %w", identityDir, err)
	}
	log.Info("identity loaded", logger.WithString("opt_identity", identityKey.ID.String()))

	cn, err := connmgr.NewConnManager(5, 50, connmgr.WithGracePeriod(30*time.Second))
	if err != nil {
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}

	cachedAddrs := commonnet.MustBuildAdvertisedQUICAddresses(log, publicIPV4, publicIPV6, cfg.ListenPort)

	libP2POpts := []libp2p.Option{
		libp2p.ConnectionManager(cn),
		libp2p.QUICReuse(quicreuse.NewConnManager),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", cfg.ListenPort),
			fmt.Sprintf("/ip6/::/udp/%d/quic-v1", cfg.ListenPort),
		),
		libp2p.Ping(false), // Disable Ping Service.
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.Identity(key),
		libp2p.AddrsFactory(func([]multiaddr.Multiaddr) []multiaddr.Multiaddr {
			return cachedAddrs
		}),
	}
	if cfg.CustomConnectionGater != nil {
		libP2POpts = append(libP2POpts, libp2p.ConnectionGater(cfg.CustomConnectionGater))
	}
	if telemetry.MetricsEnabled() {
		libP2POpts = append(libP2POpts,
			libp2p.PrometheusRegisterer(telemetry.CustomRegistry),
			libp2p.BandwidthReporter(telemetry.NewBandwidthCollector()),
		)
	}
	h, err := libp2p.New(libP2POpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create optimum libp2p host: %w", err)
	}
	if telemetry.MetricsEnabled() {
		h.Network().Notify(telemetry.NewConnectionsMeeter())
	}
	return NewNodeWithHost(ctx, log, cfg, h, identityDir, opts...)
}

// NewNodeWithHost creates a new node instance using a custom libp2p host.
// It initializes the mump2p pub-sub instance and sets up the node with the provided configuration.
func NewNodeWithHost(
	ctx context.Context,
	log logger.AppLogger,
	cfg *Config,
	h host.Host,
	identityDir string,
	opts ...NodeOption,
) (*Node, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}
	resolved := resolveOptions(cfg, opts)

	ret := &Node{
		ctx:              ctx,
		host:             h,
		log:              log,
		cfg:              cfg,
		topics:           syncx.NewRWMap[string, *pubsub.Topic](),
		subscriptions:    syncx.NewRWMap[string, *pubsub.Subscription](),
		broadcaster:      syncx.NewBroadcaster[*entities.MumP2PResponse](),
		peersMap:         syncx.NewTTLMap[peer.ID, entities.PeerState](15*time.Second, 15*time.Second),
		peersApprovedMap: syncx.NewRWMap[peer.ID, struct{}](),
		tk:               topics_keeper.NewService(ctx, log.With(logger.WithService("topic_keeper")), identityDir),
		handshakeBuilder: resolved.handshakeBuilder,
		handshakeHandler: resolved.handshakeHandler,
	}

	log.Info("initializing mump2p gossipsub")
	if _, err := utils.CalculateMaxSize(cfg.MaxMessageSize); err != nil {
		return nil, fmt.Errorf("failed to calculate max message size: %w", err)
	}

	nodeCfg, cfgErr := toNodeConfig(cfg)
	if cfgErr != nil {
		log.Error("failed to apply dynamic mump2p config", cfgErr)
	}
	slogger := nodeLogger(cfg.ClusterID)

	coder := resolved.coder
	if coder == nil {
		var err error
		if coder, err = newSHMCoder(nodeCfg, slogger); err != nil {
			return nil, err
		}
	}

	if err := ret.startPubSub(ctx, nodeCfg, slogger, h, coder); err != nil {
		return nil, err
	}

	ret.RegisterHandshakeMessageSender(cfg.ClusterID)
	ret.RegisterHandshakeHandler(cfg.ClusterID)
	go ret.DumpState(cfg.ClusterID)
	if len(cfg.BootstrapPeers) > 0 {
		var err error
		if ret.bootstrapNodes, err = utils.PeersFromStrings(cfg.BootstrapPeers); err != nil {
			return nil, fmt.Errorf("failed to parse bootstrap peers: %w", err)
		}
	}

	for _, topic := range ret.tk.GetAllTopics() {
		if err := ret.SubscribeTopic(topic); err != nil {
			log.Error("failed to subscribe to topic from topics keeper", err, logger.WithTopic(topic))
			continue
		}
		log.Info("subscribed to topic from topics keeper", logger.WithTopic(topic))
	}
	return ret, nil
}

// startPubSub builds the RLNC pubsub and retains the router and datagram handles
// the node has to close on Stop.
func (n *Node) startPubSub(
	ctx context.Context,
	nodeCfg *mp2pconfig.Config,
	slogger *slog.Logger,
	h host.Host,
	coder Coder,
) error {
	cats := entities.NewTraceCategories(n.cfg.TraceMesh, n.cfg.TraceRPC, n.cfg.TraceShard)

	psOpts := []rlncps.RLNCOption{
		rlncps.WithRLNCTracer(tracer.NewTracerMumP2P(n.broadcaster, cats)),
		rlncps.WithRawTracer(tracer.NewRawTracerMumP2P(n.broadcaster, cats)),
		// Default-deny mesh admission (#923): only a peer whose handshake verified
		// on this connection is staged, sent to, or accepted from.
		rlncps.WithPeerAdmission(n.HandshakeVerified),
	}
	if telemetry.MetricsEnabled() {
		psOpts = append(psOpts, rlncps.WithRawTracer(telemetry.NewMumP2PCollector()))
	}

	ps, router, dgram, err := rlncps.NewRLNCPubSub(ctx, nodeCfg, slogger, h, coder, psOpts...)
	if err != nil {
		return fmt.Errorf("failed to create mump2p: %w", err)
	}
	n.ps, n.router, n.dgram = ps, router, dgram
	return nil
}

// resolveOptions applies the caller's options over the defaults, which carry the
// gateway's own cluster handshake.
func resolveOptions(cfg *Config, opts []NodeOption) nodeOptions {
	resolved := nodeOptions{
		handshakeBuilder: func() any {
			return entities.NewHandshake(cfg.ClusterID)
		},
		handshakeHandler: func(_ peer.ID, decoder *json.Decoder) error {
			var handshake entities.Handshake
			if errD := decoder.Decode(&handshake); errD != nil {
				return errD
			}
			if errV := handshake.Validate(cfg.ClusterID); errV != nil {
				return fmt.Errorf("validating handshake response, remote cluster `%s`: %w", handshake.ClusterID, errV)
			}
			return nil
		},
	}
	for _, opt := range opts {
		opt(&resolved)
	}
	return resolved
}

// nodeLogger returns the slog logger mump2p logs through. AppLogger does not
// expose its handler, so the node borrows the process default.
func nodeLogger(clusterID string) *slog.Logger {
	return slog.Default().With(slog.String("service", "mump2p"), slog.String("cluster_id", clusterID))
}

// Start begins the bootstrap process for the node.
func (n *Node) Start() error {
	dsc, err := discovery.New(n.ctx, n.log, n.bootstrapNodes, n.host, n.cfg.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to create discovery: %w", err)
	}
	n.oncer.Do(func() {
		go n.RetryBootstrapConnect(dsc)
	})
	return nil
}

// Stop tears the node down: the topics keeper first (waiting for any pending
// flush), then the RLNC router and datagram transport, then the host.
func (n *Node) Stop() {
	n.tk.Stop()
	if n.router != nil {
		if err := n.router.Close(); err != nil {
			n.log.Error("failed to close rlnc router", err)
		}
	}
	if err := n.dgram.Close(); err != nil {
		n.log.Error("failed to close datagram transport", err)
	}
	if err := n.host.Close(); err != nil {
		n.log.Error("failed to close host", err)
	}
}

func (n *Node) CountConnectedPeers() (totalPeers int, perTopicPeers map[string]int) {
	topics := n.ps.GetTopics()
	perTopicPeers = make(map[string]int, len(topics))
	for _, t := range topics {
		perTopicPeers[t] = len(n.ps.ListPeers(t))
	}
	return len(n.host.Network().Peers()), perTopicPeers
}

// GetMeshPeers returns the peers the node exchanges mump2p data with on a topic.
func (n *Node) GetMeshPeers(topic string) []peer.ID {
	return n.ps.ListPeers(topic)
}

// GetTopics returns list of topics the node is subscribed to.
func (n *Node) GetTopics() []string {
	return n.ps.GetTopics()
}

// GetPeers returns the list of connected peers to the node.
func (n *Node) GetPeers() []peer.ID {
	return n.host.Network().Peers()
}

// GetHost returns the libp2p host instance associated with the node.
func (n *Node) GetHost() host.Host {
	return n.host
}

// GetHostInfo returns the host's ID and addresses.
func (n *Node) GetHostInfo() peer.AddrInfo {
	return peer.AddrInfo{
		ID:    n.host.ID(),
		Addrs: n.host.Addrs(),
	}
}

func (n *Node) getPeerState(peerID peer.ID) (entities.PeerState, bool) {
	return n.peersMap.Get(peerID)
}

func (n *Node) setPeerState(peerID peer.ID, state entities.PeerState) {
	n.peersMap.Put(peerID, state)
	if state == entities.PeerStateHandshakeInvalid {
		n.peersMap.Delete(peerID)
	}
}
