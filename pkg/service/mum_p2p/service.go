package mum_p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	mplex "github.com/libp2p/go-libp2p-mplex"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	gomplex "github.com/libp2p/go-mplex"
	"github.com/multiformats/go-multiaddr"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	discovery "github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p/dhtdiscovery"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p/topics_keeper"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p/tracer"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
	pubsub "github.com/getoptimum/optimum-p2p/optimum-pubsub"
	pboptimum "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

type Node struct {
	ctx  context.Context
	log  logger.AppLogger
	cfg  *Config
	host host.Host      // The libp2p host managing network connections and identity
	ps   *pubsub.PubSub // The Optimum pub-sub instance

	tracer         *tracer.MumP2P
	bootstrapNodes []peer.AddrInfo                            // Optional bootstrap peers for initial connectivity
	topics         *syncx.RWMap[string, *pubsub.Topic]        // Active topics
	subscriptions  *syncx.RWMap[string, *pubsub.Subscription] // Active topic subscriptions
	broadcaster    *syncx.Broadcaster[*entities.MumP2PResponse]

	peersMap         *syncx.TTLMap[peer.ID, entities.PeerState]
	peersApprovedMap *syncx.RWMap[peer.ID, struct{}] // list of peers which can be used for message publish

	tk *topics_keeper.Service // Topics keeper for persisting subscribed topics. Using on node startup.

	handshakeBuilder func() any                                        // function that create handshake message
	handshakeHandler func(peerID peer.ID, decoder *json.Decoder) error // function that parse and validate handshake message

	oncer sync.Once
}

// NewNode creates a new P2P node instance using the provided config.
// It sets up the libp2p host with a listen address and initializes Optimum pub-sub.
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

	cachedAddrs := commonnet.MustBuildAdvertisedAddresses(log, publicIPV4, publicIPV6, cfg.ListenPort)

	libP2POpts := []libp2p.Option{
		libp2p.ConnectionManager(cn),
		libp2p.ListenAddrStrings(
			fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort),
			fmt.Sprintf("/ip6/::/tcp/%d", cfg.ListenPort),
		),
		libp2p.Ping(false), // Disable Ping Service.
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.DefaultMuxers,
		libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),
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
	gomplex.ResetStreamTimeout = 5 * time.Second // Configures stream timeouts on mplex
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
// It initializes the Optimum pub-sub instance and sets up the node with the provided configuration.
func NewNodeWithHost(
	ctx context.Context,
	log logger.AppLogger,
	cfg *Config,
	h host.Host,
	identityDir string,
	opts ...NodeOption,
) (ret *Node, err error) {
	if err = cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}
	ret = &Node{
		ctx:              ctx,
		host:             h,
		log:              log,
		cfg:              cfg,
		topics:           syncx.NewRWMap[string, *pubsub.Topic](),
		subscriptions:    syncx.NewRWMap[string, *pubsub.Subscription](),
		broadcaster:      syncx.NewBroadcaster[*entities.MumP2PResponse](),
		peersMap:         syncx.NewTTLMap[peer.ID, entities.PeerState](15*time.Second, 15*time.Second),
		peersApprovedMap: syncx.NewRWMap[peer.ID, struct{}](), // list of peers which can be used for message publish
		tk:               topics_keeper.NewService(ctx, log.With(logger.WithService("topic_keeper")), identityDir),

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
	ret.tracer = tracer.NewTracerMumP2P(ret.broadcaster, entities.OptimumTraceEventSet(cfg.TraceMesh, cfg.TraceRPC, cfg.TraceShard))

	log.Info("initializing optimum gossipsub")
	maxMessageSize, err := utils.CalculateMaxSize(cfg.MaxMessageSize)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate max message size: %w", err)
	}
	options := []pubsub.Option{
		// todo this for some reason not publish message to all mesh peers pubsub.WithMessageSignaturePolicy(pubsub.StrictNoSign),
		// todo this triggers panics connection manager because of missing peer.ID pubsub.WithNoAuthor(),
		pubsub.WithEventTracer(ret.tracer),
		pubsub.WithMaxMessageSize(maxMessageSize),
		pubsub.WithOptimumSubParams(toOptimumConfig(cfg)),
		pubsub.WithConfigRotator(cfg.Rotator),
		// Default-deny mesh admission (#923): a peer joins the mesh only after its
		// handshake verifies (AllowPeer in handshake.go), not merely on connect.
		pubsub.WithPeerAdmissionControl(),
		pubsub.WithPeerMsgFilter(func(pid peer.ID, _ string) bool {
			_, ok := ret.peersApprovedMap.Load(pid)
			return ok
		}),
	}
	if telemetry.MetricsEnabled() {
		options = append(options, pubsub.WithRawTracer(telemetry.NewMumP2PCollector()))
	}

	// NN: set custom message ID function for optimump2p to use SHA256 hash of the message data
	options = append(options, pubsub.WithMessageIdFn(func(msg *pboptimum.Message) string {
		if msg == nil {
			return ""
		}
		return commonhash.SHA256(msg.Data)
	}))

	// Create pubsub before registering handshake handlers so ps is set when the first
	// handshake calls AllowPeer (#923); otherwise the peer is deferred for its lifetime.
	ret.ps, err = pubsub.NewOptimumP2P(ctx, h, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create mump2p: %w", err)
	}

	ret.RegisterHandshakeMessageSender(cfg.ClusterID)
	ret.RegisterHandshakeHandler(cfg.ClusterID)
	go ret.DumpState(cfg.ClusterID)
	if len(cfg.BootstrapPeers) > 0 {
		ret.bootstrapNodes, err = utils.PeersFromStrings(cfg.BootstrapPeers)
		if err != nil {
			return nil, fmt.Errorf("failed to parse bootstrap peers: %w", err)
		}
	}

	for _, topic := range ret.tk.GetAllTopics() {
		if err = ret.SubscribeTopic(topic); err != nil {
			log.Error("failed to subscribe to topic from topics keeper", err, logger.WithTopic(topic))
			continue
		}
		log.Info("subscribed to topic from topics keeper", logger.WithTopic(topic))
	}
	for _, opt := range opts {
		opt(ret)
	}
	return ret, nil
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

// Stop stops the topics keeper (waiting for any pending flush) and closes the host.
func (n *Node) Stop() {
	n.tk.Stop()
	if err := n.host.Close(); err != nil {
		n.log.Error("failed to close host", err)
	}
}

func (n *Node) CountConnectedPeers() (totalPeers int, perTopicPeers map[string]int) {
	topics := n.ps.GetTopics()
	perTopicPeers = make(map[string]int, len(topics))
	for _, t := range topics {
		perTopicPeers[t] = len(n.ps.GetMeshPeers(t))
	}
	return len(n.host.Network().Peers()), perTopicPeers
}

// GetMeshPeers returns the list of peer in the state variable mesh[topic] at the node.
func (n *Node) GetMeshPeers(topic string) []peer.ID {
	return n.ps.GetMeshPeers(topic)
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
	switch state {
	case entities.PeerStateHandshakeValid:
		n.peersApprovedMap.Store(peerID, struct{}{})
	case entities.PeerStateHandshakeInvalid:
		n.peersMap.Delete(peerID)
		n.peersApprovedMap.Delete(peerID)
	}
}
