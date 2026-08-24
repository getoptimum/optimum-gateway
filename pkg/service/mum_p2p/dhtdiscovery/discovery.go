package discovery

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	kaddht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	p2pdisc "github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	p2pdiscr "github.com/libp2p/go-libp2p/p2p/discovery/routing"

	commonhash "github.com/getoptimum/optimum-common/pkg/hash"
	"github.com/getoptimum/optimum-common/pkg/logger"
)

const (
	defaultConnectionTimeOut        = 30 * time.Second
	defaultFindPeersRetryDelay      = 5 * time.Second
	defaultInternalDHTAdvertisement = 2 * time.Hour

	baseDiscoveryNameSpace = "optimum-disc"
	baseBootstrapProtocol  = "/optimumkad"
)

// Discovery handles bootstrapping, advertising, and peer discovery over libp2p DHT.
type Discovery struct {
	ctx context.Context  // Shared context
	log logger.AppLogger // Structured logger

	h    host.Host         // Local libp2p host instance
	dht  *kaddht.IpfsDHT   // Underlying Kademlia DHT instance
	disc p2pdisc.Discovery // Routing-based discovery mechanism

	connectTimeout      time.Duration   // Timeout for connecting to bootstrap peers
	bootNodes           []peer.AddrInfo // List of bootstrap peer addresses
	findPeersRetryDelay time.Duration   // Delay between peer discovery attempts
	advertiseInterval   time.Duration   // Base interval between DHT advertisements

	readyToAdvertise chan struct{} // Signal channel to trigger advertisement

	// custom versions with suffix for DHT protocol to avoid conflicts in shared networks
	customDHTProtocol  protocol.ID
	customDHTNamespace string
}

// New creates a new Discovery service using the provided host and bootstrap peers.
func New(
	ctx context.Context,
	log logger.AppLogger,
	bootNodes []peer.AddrInfo,
	h host.Host,
	customDHTProtocolSuffix string,
) (*Discovery, error) {
	suffix := GetSuffix(customDHTProtocolSuffix)
	d := Discovery{
		ctx:                 ctx,
		log:                 log,
		h:                   h,
		connectTimeout:      defaultConnectionTimeOut,
		findPeersRetryDelay: defaultFindPeersRetryDelay,
		bootNodes:           bootNodes,
		advertiseInterval:   defaultInternalDHTAdvertisement,
		readyToAdvertise:    make(chan struct{}),

		customDHTNamespace: baseDiscoveryNameSpace + suffix,
		customDHTProtocol:  protocol.ID(baseBootstrapProtocol + suffix),
	}

	if len(d.bootNodes) == 0 {
		d.log.Info("no bootNodes in the config")
	}
	if err := d.setupDHT(d.ctx); err != nil {
		return nil, err
	}
	return &d, nil
}

// FindPeer attempts to find a peer by ID via the DHT.
func (d *Discovery) FindPeer(ctx context.Context, p peer.ID) (peer.AddrInfo, error) {
	return d.dht.FindPeer(ctx, p)
}

// Start begins bootstrapping, advertising, and peer discovery loops.
func (d *Discovery) Start() (chan peer.AddrInfo, error) {
	d.connectToBootstrapNodes()
	if err := d.dht.Bootstrap(d.ctx); err != nil {
		d.log.Error("unexpected error from discovery dht", err)
		return nil, err
	}
	go d.advertiseNode()
	return d.discoverPeers(), nil
}

// connectToBootstrapNodes attempts to establish initial peer connections.
func (d *Discovery) connectToBootstrapNodes() {
	ctx, cancel := context.WithTimeout(d.ctx, d.connectTimeout)
	defer cancel()

	var wg sync.WaitGroup
	for i := range d.bootNodes {
		if d.bootNodes[i].ID == d.h.ID() {
			continue // not dialing self
		}
		wg.Add(1)
		d.log.Info("Attempting to connect to bootstrap node", logger.WithPeer(d.bootNodes[i]))

		go func(key int) {
			defer wg.Done()
			if err := d.h.Connect(ctx, d.bootNodes[key]); err != nil {
				if ctx.Err() != nil {
					d.log.Debug("bootstrap dial stopped", logger.WithError(err), logger.WithPeer(d.bootNodes[key]))
					return
				}
				d.log.Error("failed to connect", err, logger.WithPeer(d.bootNodes[key]))
			}
		}(i)
	}
	wg.Wait()
}

// setupDHT initializes the Kademlia DHT and its underlying LevelDB datastore.
func (d *Discovery) setupDHT(ctx context.Context) (err error) {
	opts := []kaddht.Option{
		kaddht.Validator(record.PublicKeyValidator{}),
		kaddht.ProtocolPrefix(d.customDHTProtocol),
		kaddht.Mode(kaddht.ModeAutoServer),
		kaddht.BootstrapPeers(d.bootNodes...),
	}

	d.dht, err = kaddht.New(ctx, d.h, opts...)
	if err != nil {
		return fmt.Errorf("error creating DHT: %w", err)
	}
	d.disc = p2pdiscr.NewRoutingDiscovery(d.dht)
	return nil
}

// advertiseNode advertises the node to the network with a different interval after each advertisement
func (d *Discovery) advertiseNode() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case <-d.readyToAdvertise:
			peersLen := len(d.h.Network().Peers())
			routablePeers := d.dht.RoutingTable().Size()
			l := d.log.With(
				logger.WithString("ns", d.customDHTNamespace),
				logger.WithInt("peers", peersLen),
				logger.WithInt("routable_peers", routablePeers),
			)
			l.Info("starting node advertisement")
			if peersLen == 0 || routablePeers == 0 {
				l.Info("no peers connected, skipping advertisement")
				continue
			}
			ttl, err := d.disc.Advertise(d.ctx, d.customDHTNamespace, p2pdisc.TTL(d.advInterval()))
			if err != nil {
				l.Error("error advertising node", err)
				time.Sleep(15 * time.Second)
			}
			select {
			case <-d.ctx.Done():
				return
			case <-time.After(ttl):
			}
		}
	}
}

// discoverPeers periodically discovers peers providing a service
// mark to advertiseNode goroutine that it is ready to advertise
func (d *Discovery) discoverPeers() chan peer.AddrInfo {
	peerCh := make(chan peer.AddrInfo)
	go func() {
		defer close(peerCh)

		ticker := time.NewTicker(d.findPeersRetryDelay)
		defer ticker.Stop()
		for {
			select {
			case <-d.ctx.Done():
				return
			case <-ticker.C:
				if err := d.findPeers(d.customDHTNamespace, peerCh); err != nil {
					d.log.Error("error discovering peers", err, logger.WithString("ns", d.customDHTNamespace))
					continue
				}
				// we found peers, after this we self advertise
				// after advertisement we delay next advertisement by ttl returned from advertise call
				// this way we avoid advertising too frequently
				// we use routing discovery which relies on DHT, so if we advertise too frequently it can overload DHT
				// by using non-buffered channel we ensure that we only advertise after we found peers
				// and we wait for ttl duration before next peers lookup / advertisement
				d.readyToAdvertise <- struct{}{}
			}
		}
	}()
	return peerCh
}

func (d *Discovery) findPeers(ns string, out chan<- peer.AddrInfo) error {
	d.log.Info("started finding peers", logger.WithString("ns", ns))
	defer d.log.Info("done finding peers", logger.WithString("ns", ns))

	ctx, cancel := context.WithTimeout(d.ctx, 15*time.Second)
	defer cancel()

	peerCh, err := d.disc.FindPeers(ctx, ns)
	if err != nil {
		return err
	}
	for p := range peerCh {
		if p.ID == d.h.ID() {
			continue // skip self
		}
		out <- p
		d.log.Info("discovered peer", logger.WithString("ns", ns), logger.WithPeer(p))
	}
	return nil
}

// advInterval returns the advertise interval for the discovery service.
// in Kademlia DHT nodes advertise themselves periodically to make their presence known to others in the network
// randomization of interval is done to avoid all nodes advertising at the same time
// * too many nodes advertising simultaneously can overwhelm the network
func (d *Discovery) advInterval() time.Duration {
	tmp := d.advertiseInterval / 2
	maxVal := big.NewInt(int64(2 * tmp))
	n, err := rand.Int(rand.Reader, maxVal)
	if err != nil {
		d.log.Fatal("error generating random number", err) // ok fatal here
	}
	result := d.advertiseInterval - tmp + time.Duration(n.Int64())
	d.log.Info("next advertise interval", logger.WithString("interval", result.String()))
	return result
}

func GetSuffix(customDHTProtocolSuffix string) string {
	customDHTProtocolSuffix = strings.TrimSpace(customDHTProtocolSuffix)
	if customDHTProtocolSuffix == "" {
		return ""
	}
	result := commonhash.SHA256([]byte(customDHTProtocolSuffix))
	if len(result) < 16 {
		return "" // expect 64 hex symbols, so it unlikely
	}
	return result[len(result)-16:]
}
