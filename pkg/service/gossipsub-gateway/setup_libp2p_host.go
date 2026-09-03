package gossipsub_gateway

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ipfs/go-log"
	"github.com/libp2p/go-libp2p"
	mplex "github.com/libp2p/go-libp2p-mplex"
	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	libp2ptcp "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	gomplex "github.com/libp2p/go-mplex"
	"github.com/multiformats/go-multiaddr"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-common/pkg/pointers"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry/tracer"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	// overlay parameters
	gossipSubD   = 8  // topic stable mesh target count
	gossipSubDlo = 6  // topic stable mesh low watermark
	gossipSubDhi = 12 // topic stable mesh high watermark

	// gossip parameters
	gossipSubMcacheLen    = 6                    // number of windows to retain full messages in cache for `IWANT` responses
	gossipSubMcacheGossip = 3                    // number of windows to gossip about
	gossipSubSeenTTL      = 12 * 3 * time.Second // TTL for seen message IDs: 3 slots * 12s/slot = 36sec

	// heartbeat interval
	gossipSubHeartbeatInterval = 700 * time.Millisecond // frequency of heartbeat, milliseconds
	// direct peer reconnection
	gossipSubDirectConnectTicks = 12 // number of heartbeat ticks between reconnection attempts for direct peers (~12s with 700ms heartbeat)
)

// creates a custom gossipsub parameter set.
func pubsubGossipParam() libp2ppubsub.GossipSubParams {
	gParams := libp2ppubsub.DefaultGossipSubParams()
	gParams.Dlo = gossipSubDlo
	gParams.D = gossipSubD
	gParams.Dhi = gossipSubDhi
	gParams.HeartbeatInterval = gossipSubHeartbeatInterval
	gParams.HistoryLength = gossipSubMcacheLen
	gParams.HistoryGossip = gossipSubMcacheGossip
	return gParams
}

// debugLibP2P enables verbose logging for all libp2p subsystems. Activate via LIBP2P_DEBUG=1 env var.
func debugLibP2P() {
	for _, sub := range []string{
		"swarm2", "connmgr", "net/identify", "pubsub", "peerstore",
		"mux", "mplex", "yamux", "nat", "ping", "gateway", "noise", "tls",
	} {
		_ = log.SetLogLevel(sub, "DEBUG")
	}
}

func (s *Service) setupLibP2PHost() error {
	if os.Getenv("LIBP2P_DEBUG") == "1" {
		debugLibP2P()
	}
	key, err := identity.EnsureIdentity(s.cfg.IdentityLibP2PDir)
	if err != nil {
		return fmt.Errorf("could not create libp2p identity: %w", err)
	}

	// Listen on all interfaces (0.0.0.0 for IPv4, :: for IPv6) to accept connections from localhost, Docker networks, and external IPs
	listenAddrIPv4, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", s.cfg.AgentLibP2PPort))
	if err != nil {
		return fmt.Errorf("failed to create IPv4 listen multiaddr: %w", err)
	}
	listenAddrIPv6, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip6/::/tcp/%d", s.cfg.AgentLibP2PPort))
	if err != nil {
		return fmt.Errorf("failed to create IPv6 listen multiaddr: %w", err)
	}
	listenAddrs := []multiaddr.Multiaddr{listenAddrIPv4, listenAddrIPv6}

	var publicIP string
	if s.cfg.AnnounceIP != "" {
		s.log.Info("using configured announce_ip override for CL-facing host, skipping autodetection",
			logger.WithString("announce_ip", s.cfg.AnnounceIP),
		)
		publicIP = s.cfg.AnnounceIP
	} else {
		var err error
		publicIP, _, err = commonnet.GetExternalIPs()
		if err != nil {
			return fmt.Errorf("failed to get outbound IP address: %w", err)
		}
	}

	internalIPs, err := commonnet.GetPrivateIPs()
	if err != nil {
		return fmt.Errorf("failed to get private IPs: %w", err)
	}

	resultAddrFactory := make([]multiaddr.Multiaddr, 0, len(internalIPs))
	for _, internalIP := range internalIPs {
		resultAddrFactory = append(resultAddrFactory, multiaddr.StringCast(fmt.Sprintf("/ip4/%s/tcp/%d", internalIP, s.cfg.AgentLibP2PPort)))
	}
	if publicIP != "" { // Add public IP address if available
		ipProto := commonnet.GetIPProtocol(publicIP)
		resultAddrFactory = append(resultAddrFactory, multiaddr.StringCast(fmt.Sprintf("/%s/%s/tcp/%d", ipProto, publicIP, s.cfg.AgentLibP2PPort)))
	}

	libP2POpts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Identity(key),
		libp2p.DisableRelay(),
		libp2p.Transport(libp2ptcp.NewTCPTransport),
		libp2p.DefaultMuxers,
		libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Ping(false), // Disable Ping Service.
		libp2p.AddrsFactory(func([]multiaddr.Multiaddr) []multiaddr.Multiaddr {
			return resultAddrFactory
		}),
	}
	gomplex.ResetStreamTimeout = 5 * time.Second // Configures stream timeouts on mplex
	s.hostLibP2P, err = libp2p.New(libP2POpts...)
	if err != nil {
		return fmt.Errorf("unable to create libp2p host: %w", err)
	}

	go s.handleHandshake()

	// Initialize peer topic tracer to discover topics from connected peers
	// When a topic has 0 peers, unsubscribe the gateway from that topic
	s.peerTopicTracer = tracer.NewPeerTopicTracer(s.log, s.GetForkDigestManager(), s.onZeroPeersForTopic)

	options := []libp2ppubsub.Option{
		libp2ppubsub.WithMessageSignaturePolicy(libp2ppubsub.StrictNoSign),
		libp2ppubsub.WithNoAuthor(),
		libp2ppubsub.WithSeenMessagesTTL(gossipSubSeenTTL),
		libp2ppubsub.WithMessageIdFn(s.pubsubMsgID),
		libp2ppubsub.WithGossipSubParams(pubsubGossipParam()),
		libp2ppubsub.WithPeerExchange(false),
		libp2ppubsub.WithMaxMessageSize(1024 * 1024),
		libp2ppubsub.WithEventTracer(s.peerTopicTracer),
	}
	directCLPeers := make([]peer.AddrInfo, 0)
	if len(s.cfg.DirectCLPeers) > 0 {
		directCLPeers, err = utils.PeersFromStrings(s.cfg.DirectCLPeers)
		if err != nil {
			return fmt.Errorf("unable to parse direct CL peers: %w", err)
		}
	}
	s.directCLPeersAllowlist = make(map[peer.ID]struct{}, len(directCLPeers))
	for _, directPeer := range directCLPeers {
		s.directCLPeersAllowlist[directPeer.ID] = struct{}{}
	}
	if len(directCLPeers) > 0 {
		options = append(options,
			libp2ppubsub.WithDirectPeers(directCLPeers),
			libp2ppubsub.WithDirectConnectTicks(gossipSubDirectConnectTicks),
		)
		s.log.Info("adding direct CL peers to libp2p pubsub", logger.WithInt("count", len(directCLPeers)))
	}

	s.ps, err = libp2ppubsub.NewGossipSub(s.ctx, s.hostLibP2P, options...)
	if err != nil {
		return fmt.Errorf("unable to create agent_p2p pubsub service: %w", err)
	}

	s.hostLibP2P.Network().Notify(
		&network.NotifyBundle{
			ConnectedF:    s.onPeerConnected,
			DisconnectedF: s.onPeerDisconnected,
		},
	)

	s.log.Info("agent_p2p service host created",
		logger.WithString("host_id", s.hostLibP2P.ID().String()),
		logger.WithString("listen_addrs", fmt.Sprintf("%v", listenAddrs)),
	)
	return nil
}

// onZeroPeersForTopic is invoked by the peer topic tracer when a topic drops to 0 discovered peers.
// It unsubscribes the gateway from that topic, but only if it is currently subscribed.
func (s *Service) onZeroPeersForTopic(topic string) {
	// Only unsubscribe if we're actually subscribed to this topic
	if _, ok := s.libP2PSubs.Load(topic); ok {
		s.log.Info("unsubscribing from topic (0 peers discovered)", logger.WithTopic(topic))
		s.unsubscribeFromTopic(topic)
	}
}

// pubsubMsgID computes the gossipsub message ID based on the topic and message data.
func (s *Service) pubsubMsgID(pmsg *pubsubpb.Message) string {
	return consensus.MsgID(pointers.FromPointer(pmsg.Topic), pmsg.Data)
}

// onPeerConnected disconnects peers not listed in direct_cl_peers when that
// config is set (connect-time allowlist, not a ConnectionGater).
func (s *Service) onPeerConnected(_ network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	if _, ok := s.directCLPeersAllowlist[peerID]; !ok && len(s.directCLPeersAllowlist) > 0 {
		_ = s.hostLibP2P.Network().ClosePeer(peerID)
		s.log.Info("disconnected peer not in direct peers allowlist", logger.WithPeerID(peerID))
		return
	}
	addr := conn.RemoteMultiaddr()
	peerAddress := peer.AddrInfo{
		ID:    peerID,
		Addrs: []multiaddr.Multiaddr{addr},
	}
	s.libP2PDirectPeers.Store(peerID.String(), peerAddress)
	telemetry.CLPeerConnected()
	go func() {
		l := s.log.With(logger.WithPeerID(peerID), logger.WithString("addr", addr.String()))
		if errD := s.ps.AddDirectPeer(peerAddress); errD != nil {
			l.Error("failed to add direct peer to pubsub", errD)
		}
		l.Info("connected to peer")

		if conn.Stat().Direction == network.DirOutbound { // Only send handshake for outgoing connections
			go func() {
				if errH := s.sendHandshakeForPeer(s.ctx, conn.RemotePeer()); errH != nil {
					l.Error("failed to send handshake for peer", errH)
					return
				}
				s.subscribeOnce()
			}()
		}

		l.Info("storing peer connection")
		s.hostLibP2P.Peerstore().AddAddr(peerID, addr, peerstore.ConnectedAddrTTL)
		s.hostLibP2P.Peerstore().AddAddrs(peerID, s.hostLibP2P.Peerstore().Addrs(peerID), peerstore.ConnectedAddrTTL)
	}()
}

// onPeerDisconnected is the DisconnectedF notifiee callback. It clears peerstore and direct peer
// state once the peer is fully disconnected and records the disconnect for telemetry.
func (s *Service) onPeerDisconnected(n network.Network, conn network.Conn) {
	peerID := conn.RemotePeer()
	if n.Connectedness(peerID) != network.Connected {
		s.libP2PDirectPeers.Delete(peerID.String())
		s.hostLibP2P.Peerstore().ClearAddrs(peerID)
		s.hostLibP2P.Peerstore().RemovePeer(peerID)
	}
	telemetry.CLPeerDisconnected()
}

func (s *Service) sendHandshakeForPeer(ctx context.Context, peerID peer.ID) error {
	netStatus := s.networkStatus.Load()
	if netStatus == nil {
		return nil
	}

	stream, err := s.hostLibP2P.NewStream(ctx, peerID, protocol.ID(netStatus.protocol))
	if err != nil {
		return fmt.Errorf("unable to create stream: %w", err)
	}
	defer stream.Close()
	if _, err = s.sszEncoder.EncodeWithMaxLength(stream, netStatus.status); err != nil {
		return fmt.Errorf("unable to send handshake data to peer: %w", err)
	}

	if err = stream.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil &&
		!strings.Contains(err.Error(), "stream closed") {
		return fmt.Errorf("unable to set read deadline: %w", err)
	}
	b := make([]byte, 1)
	if _, err = stream.Read(b); err != nil {
		return fmt.Errorf("failed to read response from peer %s: %w", peerID, err)
	}
	if b[0] == entities.ResponseCodeSuccess {
		msg := newStatusMessage(netStatus.protocol)
		if msg == nil {
			return fmt.Errorf("unsupported status protocol %q", netStatus.protocol)
		}
		if err = s.sszEncoder.DecodeWithMaxLength(stream, msg); err != nil {
			return fmt.Errorf("failed to decode status at handshake: %w", err)
		}
	} else {
		return fmt.Errorf("peer %s returned error code: %d", peerID, b[0])
	}
	return nil
}
