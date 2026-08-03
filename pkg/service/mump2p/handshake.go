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

	rlncps "github.com/getoptimum/mump2p-protocol/pkg/pubsub"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

const (
	timeForHandshake = 10 * time.Second // Time to wait for a handshake response before disconnecting

	// timeout is time to wait for handshake to be marked valid
	timeout = 15 * time.Second

	// interval for checking handshake status
	interval = 1 * time.Second

	// datagramEstablishAttempts and datagramEstablishRetryDelay bound the retry of
	// the datagram session handshake. The window being covered is the remote's own
	// admission, which is one stream read away, so a couple of seconds is generous;
	// a peer that has still not admitted us by then is not going to.
	datagramEstablishAttempts   = 5
	datagramEstablishRetryDelay = 500 * time.Millisecond

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
			DisconnectedF: func(net network.Network, conn network.Conn) {
				peerID := conn.RemotePeer()
				n.log.With(
					logger.WithString("flow", "notify_disconnected"),
					logger.WithPeerID(peerID),
					logger.WithString("addr", conn.RemoteMultiaddr().String()),
					logger.WithString("direction", conn.Stat().Direction.String()),
					logger.WithString("opened", conn.Stat().Opened.Format(time.DateTime)),
				).Debug("disconnected from peer")

				// A disconnect for a closed connection can land after the peer is
				// already back on a new one. Revoking then would deny a peer that
				// will not handshake again, partitioning it for that connection.
				if net.Connectedness(peerID) != network.NotConnected {
					return
				}
				n.revokeAdmission(peerID)
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
	result, err := n.decodeHandshake(remotePeer, stream)
	if err != nil {
		n.disconnectPeer(remotePeer)
		return fmt.Errorf("verifying handshake response: %w", err)
	}

	n.markHandshakeValid(remotePeer, result)
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
		result, err := n.decodeHandshake(remotePeer, stream)
		if err != nil {
			l.Error("handshake verification failed", err)
			n.disconnectPeer(remotePeer)
			return
		}

		if err := json.NewEncoder(stream).Encode(n.handshakeBuilder()); err != nil {
			l.Error("failed to send handshake response", err)
			n.disconnectPeer(remotePeer)
			return
		}
		n.markHandshakeValid(remotePeer, result)
		l.Info("handshake handled successfully")
	})
}

// decodeHandshake bounds the read at maxHandshakeBytes before the handler decodes it, so a
// peer that has not been authenticated yet cannot grow the decoder's buffer without limit.
func (n *Node) decodeHandshake(peerID peer.ID, r io.Reader) (HandshakeResult, error) {
	// One byte of slack: an exhausted N then means the peer went over the cap, not that it
	// happened to send exactly maxHandshakeBytes.
	limited := &io.LimitedReader{R: r, N: maxHandshakeBytes + 1}
	result, err := n.handshakeHandler(peerID, json.NewDecoder(limited))
	if err != nil && limited.N <= 0 {
		// Truncation surfaces as "unexpected EOF", which hides the real cause during triage.
		return HandshakeResult{}, fmt.Errorf("handshake payload exceeds %d byte limit: %w", maxHandshakeBytes, err)
	}
	return result, err
}

// markHandshakeValid records a verified handshake and admits the peer to the
// mesh (#923).
func (n *Node) markHandshakeValid(peerID peer.ID, result HandshakeResult) {
	n.setPeerState(peerID, entities.PeerStateHandshakeValid)
	n.peersApprovedMap.Store(peerID, result.TokenExpiry)
	// Admission is only consulted while a peer is being staged, and this peer was
	// skipped when it connected unauthorized. Nothing else re-queues it, so
	// without this it stays out of the peer set for the life of the connection.
	rlncps.RestagePeer(n.ps, peerID)
	n.establishDatagramSession(peerID)
}

// HandshakeVerified reports whether peerID currently holds a verified handshake
// on its live connection. It is the default-deny mesh admission predicate
// (#923), so it is consulted on the pubsub processLoop and on the receive path:
// it must stay a plain map read.
func (n *Node) HandshakeVerified(peerID peer.ID) bool {
	_, ok := n.peersApprovedMap.Load(peerID)
	return ok
}

// PeerAuthorization reports whether peerID is admitted and when the credential
// that admitted it expires. A zero expiry means the handshake verified no
// credential, so there is nothing for a session to be capped against.
func (n *Node) PeerAuthorization(peerID peer.ID) (time.Time, bool) {
	return n.peersApprovedMap.Load(peerID)
}

// establishDatagramSession opens the peer's datagram keys off the handshake
// goroutine: establishment dials a second stream, and on the inbound path the
// handshake response has not been written yet.
//
// It retries because the two sides do not admit each other at the same instant.
// Which side dials the session is decided by peer ID, not by who ran the cluster
// handshake, so the dialing side is often the handshake RESPONDER, which admits
// as soon as it has written its response, while the handshake initiator only
// admits once it has read that response back. In that window the responder's
// dial lands on a peer that has not admitted it yet and is refused. A single
// attempt then leaves the pair with no session at all, and nothing fails: every
// send between them silently takes the stream fallback for the life of the
// connection.
func (n *Node) establishDatagramSession(peerID peer.ID) {
	if n.udp == nil {
		return
	}
	go func() {
		for attempt := 1; ; attempt++ {
			err := n.udp.Establish(n.ctx, peerID)
			if err == nil {
				return
			}
			if attempt >= datagramEstablishAttempts {
				n.log.Error("failed to establish datagram session", err,
					logger.WithPeerID(peerID), logger.WithInt("attempts", attempt))
				return
			}
			select {
			case <-n.ctx.Done():
				return
			case <-time.After(datagramEstablishRetryDelay):
			}
		}
	}()
}

// revokeAdmission drops a peer from the mesh allow set.
//
// The cached valid handshake goes with it, so the next connection runs a fresh
// handshake instead of short-circuiting it and never re-admitting. An invalid
// marker is deliberately left in place: it still short-circuits a reconnect
// from a peer that has already been rejected.
func (n *Node) revokeAdmission(peerID peer.ID) {
	n.peersApprovedMap.Delete(peerID)
	if state, ok := n.getPeerState(peerID); ok && state == entities.PeerStateHandshakeValid {
		n.peersMap.Delete(peerID)
	}
	// Sessions are never resumed across a connection, so the keys go with the
	// connection rather than waiting for a reconnect that might reuse them.
	n.udp.Destroy(peerID)
}

func (n *Node) disconnectPeer(peerID peer.ID) {
	n.setPeerState(peerID, entities.PeerStateHandshakeInvalid)
	// Clearing the allow set before blacklisting matters: the gate records an
	// explicit, permanent denial only for a peer the predicate still admits, and
	// such an entry would survive a later successful handshake.
	n.peersApprovedMap.Delete(peerID)
	// A peer denied mid-session loses its datagram keys here, not when the
	// connection finishes closing: the key ids leave the receive table, so the
	// very next datagram it sends is dropped without being decrypted.
	n.udp.Destroy(peerID)
	// Evict from the peer set, mesh and topic state; ClosePeer alone leaves
	// pubsub-side state to be torn down by its own notifiee, later.
	if n.ps != nil {
		n.ps.BlacklistPeer(peerID)
	}
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
