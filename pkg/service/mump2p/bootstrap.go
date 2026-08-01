package mump2p

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/logger"
	discovery "github.com/getoptimum/optimum-gateway/pkg/service/mump2p/dhtdiscovery"
)

const (
	// sleepTime time to wait before retrying bootstrap connection
	sleepTime = 1 * time.Minute
)

// RetryBootstrapConnect attempts to connect to bootstrap nodes in a loop.
// If the connection fails, it will retry every minute until successful.
func (n *Node) RetryBootstrapConnect(dht *discovery.Discovery) {
	for {
		select {
		case <-n.ctx.Done():
			return
		default:
			// no bootstrap nodes are configured, act as a seed and start discovery
			if len(n.bootstrapNodes) == 0 {
				n.log.Info("acting as bootstrap seed; no bootstrap peers configured")
				go n.advertiseAndDiscover(dht)
				return
			}
			// try to connect to bootstrap nodes
			if err := n.connectToBootstrapNodes(); err != nil {
				n.log.Error("failed to connect to bootstrap nodes, will retry in 1 min", err)
				time.Sleep(sleepTime)
				continue
			}
			// connected to at least one bootstrap node, start discovery
			n.log.Info("successfully connected to bootstrap nodes")
			go n.advertiseAndDiscover(dht)
			return
		}
	}
}

// connectToBootstrapNodes attempts to connect to the provided bootstrap nodes.
// It returns nil as soon as any one connect+handshake succeeds; otherwise an error.
func (n *Node) connectToBootstrapNodes() error {
	dialCtx, cancel := context.WithTimeout(n.ctx, timeout)
	defer cancel()

	successCh := make(chan struct{}, len(n.bootstrapNodes))

	// try to connect to all bootstrap nodes in parallel
	for i := range n.bootstrapNodes {
		if n.bootstrapNodes[i].ID == n.host.ID() { // skip self
			n.log.Debug("skipping self from bootstrap list", logger.WithPeer(n.bootstrapNodes[i]))
			continue
		}

		go func(p peer.AddrInfo) { // launch a goroutine for each dial attempt
			// try to connect if not already connected
			if len(p.ID) == 0 {
				n.log.Info("boot peer has empty ID, skipping", logger.WithPeer(p))
				return
			}
			if n.host.Network().Connectedness(p.ID) != network.Connected {
				if err := n.host.Connect(dialCtx, p); err != nil {
					n.log.Error("failed to connect to bootstrap node", err, logger.WithPeer(p))
					return
				}
			}
			n.log.Info("successfully connected to bootstrap node", logger.WithPeer(p))

			// wait for handshake to be marked valid (poll, don't sleep blindly)
			if n.waitHandshakeValid(dialCtx, p.ID) {
				successCh <- struct{}{}
				return
			}
			n.log.Info("handshake not done with peer", logger.WithPeer(p))
		}(n.bootstrapNodes[i])
	}

	// return on first success, or after everyone finishes with no success
	select {
	case <-successCh:
		return nil
	case <-dialCtx.Done():
		return fmt.Errorf("bootstrap dial timed out")
	}
}

// advertiseAndDiscover starts the discovery process and connects to peers.
// It listens for new peers and connects to them if they are not already connected.
// It also handles the case where the host is already connected to a peer.
func (n *Node) advertiseAndDiscover(dsc *discovery.Discovery) {
	n.log.Info("starting discovery")
	discChan, err := dsc.Start()
	if err != nil {
		n.log.Fatal("failed to start discovery", err) // ok fatal here
	}
	for p := range discChan {
		if p.ID == n.host.ID() {
			continue // skip self
		}

		if n.host.Network().Connectedness(p.ID) == network.Connected {
			continue // skip if already connected
		}
		if len(p.ID) == 0 {
			n.log.Info("discovered peer has empty ID, skipping", logger.WithPeer(p))
			continue
		}
		ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
		err = n.host.Connect(ctx, p)
		cancel()
		if err != nil {
			n.log.Error("failed to connect to peer", err, logger.WithPeer(p))
			continue
		}
		n.log.Info("connected to new peer", logger.WithPeer(p))
	}
}
