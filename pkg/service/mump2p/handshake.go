package mump2p

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

const (
	timeForHandshake = 10 * time.Second // Time to wait for a handshake response before disconnecting

	// timeout is time to wait for handshake to be marked valid
	timeout = 15 * time.Second

	// interval for checking handshake status
	interval = 1 * time.Second

	// maxHandshakeBytes caps the pre-auth handshake read. The payload is a small JSON
	// object (cluster_id, jwt_token, commit_hash) whose only growing field is the JWT,
	// which stays under 2 KiB, so 8 KiB leaves ample headroom while keeping a peer whose
	// token is not yet verified from streaming unbounded data into the decoder.
	maxHandshakeBytes = 8 << 10
)

// RegisterHandshakeMessageSender registers a notification handler for new peer connections.
// When a new peer connects, it sends a handshake message to the peer if it is not already in a valid handshake state.
// if no handshake message in 10 seconds, it will disconnect the peer.
func (n *Node) RegisterHandshakeMessageSender(clusterID string) {
	n.host.Network().Notify(
		&network.NotifyBundle{
			ConnectedF: func(_ network.Network, conn network.Conn) {
				go n.handleNewConnection(clusterID, conn)
			},
			DisconnectedF: func(_ network.Network, conn network.Conn) {
				n.log.With(
					logger.WithString("flow", "notify_disconnected"),
					logger.WithPeerID(conn.RemotePeer()),
					logger.WithString("addr", conn.RemoteMultiaddr().String()),
					logger.WithString("direction", conn.Stat().Direction.String()),
					logger.WithString("opened", conn.Stat().Opened.Format(time.DateTime)),
				).Debug("disconnected from peer")
			},
		},
	)
}

func (n *Node) handleNewConnection(clusterID string, conn network.Conn) {
	peerID := conn.RemotePeer()
	l := n.log.With(
		logger.WithFlow("handshake"),
		logger.WithString("protocol", "outgoing_handshake"),
		logger.WithPeerID(peerID),
		logger.WithString("remote_addr", conn.RemoteMultiaddr().String()),
		logger.WithString("direction", conn.Stat().Direction.String()),
		logger.WithClusterID(clusterID),
		logger.WithString("run_id", uuid.NewString()[:8]),
	)
	state, ok := n.getPeerState(peerID)
	if ok && state == entities.PeerStateHandshakeValid {
		l.Debug("peer already has valid handshake, skipping handshake")
		return
	}
	if ok && state == entities.PeerStateHandshakeInvalid {
		l.Debug("peer marked as invalid, disconnecting immediately")
		n.disconnectPeer(peerID)
		return
	}
	l.Info("new peer connection established, starting handshake")

	ctx, cancel := context.WithTimeout(n.ctx, timeForHandshake)
	defer cancel()

	if conn.Stat().Direction == network.DirOutbound { // Outbound direction side initiates handshake
		if err := n.sendHandshakeForPeer(ctx, l, peerID); err != nil {
			l.Error("outbound handshake failed", err)
			n.disconnectPeer(peerID)
			return
		}
		l.Info("outbound handshake completed successfully")
		return // outbound handshake is done, exit now
	}

	// Inbound connection: the remote peer is responsible for opening the handshake stream
	// if it malicious node and did not open it, we wait and verify it
	if !n.waitHandshakeValid(ctx, peerID) {
		l.Info("inbound peer did not complete handshake within deadline, disconnecting")
		n.disconnectPeer(peerID)
	}
}

func (n *Node) sendHandshakeForPeer(ctx context.Context, l logger.AppLogger, pID peer.ID) error {
	childCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	topic := entities.GetHandshakeTopic(entities.HandshakeV1)
	stream, err := n.host.NewStream(childCtx, pID, protocol.ID(topic))
	if err != nil {
		return fmt.Errorf("sending handshake message: %w", err)
	}
	defer stream.Close()
	remotePeer := stream.Conn().RemotePeer()
	setRPCStreamDeadlines(l, stream)

	if err = json.NewEncoder(stream).Encode(n.handshakeBuilder()); err != nil {
		n.disconnectPeer(remotePeer)
		return fmt.Errorf("encoding handshake message: %w", err)
	}

	if err = stream.CloseWrite(); err != nil {
		n.disconnectPeer(remotePeer)
		return fmt.Errorf("closing write stream: %w", err)
	}

	// Wait for the response from the peer and verify handshake
	if err = n.decodeHandshake(remotePeer, stream); err != nil {
		n.disconnectPeer(remotePeer)
		return fmt.Errorf("verifying handshake response: %w", err)
	}

	n.markHandshakeValid(remotePeer)
	return nil
}

// RegisterHandshakeHandler registers a stream handler for the handshake protocol exchange.
func (n *Node) RegisterHandshakeHandler(clusterID string) {
	topic := entities.GetHandshakeTopic(entities.HandshakeV1)
	n.host.SetStreamHandler(protocol.ID(topic), func(stream network.Stream) {
		l := n.log.With(
			logger.WithString("protocol", "incoming_handshake"),
			logger.WithFlow("handshake"),
			logger.WithClusterID(clusterID),
			logger.WithPeerID(stream.Conn().RemotePeer()),
		)

		defer stream.Close()
		setRPCStreamDeadlines(l, stream)

		remotePeer := stream.Conn().RemotePeer()
		// Read the handshake message from the stream
		if err := n.decodeHandshake(remotePeer, stream); err != nil {
			l.Error("handshake verification failed", err)
			n.disconnectPeer(remotePeer)
			return
		}

		if err := json.NewEncoder(stream).Encode(n.handshakeBuilder()); err != nil {
			l.Error("failed to send handshake response", err)
			n.disconnectPeer(remotePeer)
			return
		}
		n.markHandshakeValid(remotePeer)
		l.Info("handshake handled successfully")
	})
}

// decodeHandshake bounds the read at maxHandshakeBytes before the handler decodes it, so a
// peer that has not been authenticated yet cannot grow the decoder's buffer without limit.
func (n *Node) decodeHandshake(peerID peer.ID, r io.Reader) error {
	// One byte of slack: an exhausted N then means the peer went over the cap, not that it
	// happened to send exactly maxHandshakeBytes.
	limited := &io.LimitedReader{R: r, N: maxHandshakeBytes + 1}
	err := n.handshakeHandler(peerID, json.NewDecoder(limited))
	if err != nil && limited.N <= 0 {
		// Truncation surfaces as "unexpected EOF", which hides the real cause during triage.
		return fmt.Errorf("handshake payload exceeds %d byte limit: %w", maxHandshakeBytes, err)
	}
	return err
}

// markHandshakeValid records a verified handshake.
func (n *Node) markHandshakeValid(peerID peer.ID) {
	n.setPeerState(peerID, entities.PeerStateHandshakeValid)
}

// HandshakeVerified reports whether peerID currently holds a verified handshake.
// This is the predicate shape mump2p's mesh admission control will take once the
// replacement for the removed default-deny admission lands (#923); nothing in the
// data path consumes it yet.
func (n *Node) HandshakeVerified(peerID peer.ID) bool {
	state, ok := n.getPeerState(peerID)
	return ok && state == entities.PeerStateHandshakeValid
}

func (n *Node) disconnectPeer(peerID peer.ID) {
	n.setPeerState(peerID, entities.PeerStateHandshakeInvalid)
	if n.host.Network().Connectedness(peerID) == network.NotConnected {
		return
	}
	_ = n.host.Network().ClosePeer(peerID)
}

func setRPCStreamDeadlines(log logger.AppLogger, stream network.Stream) {
	if err := stream.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil &&
		!strings.Contains(err.Error(), "stream closed") {
		log.Error("could not set stream deadline", err)
	}
	if err := stream.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		log.Error("could not set stream deadline", err)
	}
}

// waitHandshakeValid polls the peer state until it's valid or timeout elapses.
func (n *Node) waitHandshakeValid(ctx context.Context, id peer.ID) bool {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-checkCtx.Done():
			return false
		case <-ticker.C:
			if n.HandshakeVerified(id) {
				return true
			}
		}
	}
}
