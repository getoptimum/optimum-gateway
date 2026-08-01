package udpsession

import (
	"net"
	"net/netip"

	"github.com/multiformats/go-multiaddr"
)

// LocalCandidates reports the UDP endpoints a peer should probe to reach this
// node's datagram socket.
//
// A socket bound to the unspecified address has no single answer, so the socket
// supplies the port and the addresses this node already advertises supply the
// IPs. Advertising an address costs nothing on its own: the peer still has to
// answer a probe at one before anything is sent there.
func LocalCandidates(local net.Addr, advertised []multiaddr.Multiaddr) []netip.AddrPort {
	udp, ok := local.(*net.UDPAddr)
	if !ok {
		return nil
	}

	bound := udp.AddrPort()
	if bound.Port() == 0 {
		return nil
	}

	if addr := bound.Addr().Unmap(); addr.IsValid() && !addr.IsUnspecified() {
		return []netip.AddrPort{netip.AddrPortFrom(addr, bound.Port())}
	}

	out := make([]netip.AddrPort, 0, len(advertised))
	seen := make(map[netip.AddrPort]struct{}, len(advertised))

	for _, ma := range advertised {
		ip, found := addrFromMultiaddr(ma)
		if !found || ip.IsUnspecified() {
			continue
		}

		ep := netip.AddrPortFrom(ip, bound.Port())
		if _, dup := seen[ep]; dup {
			continue
		}

		seen[ep] = struct{}{}
		out = append(out, ep)

		if len(out) == maxEndpoints {
			break
		}
	}

	return out
}

// addrFromMultiaddr pulls the IP component out of a multiaddr, ignoring the
// transport and port it was advertised with: the datagram socket has its own.
func addrFromMultiaddr(ma multiaddr.Multiaddr) (netip.Addr, bool) {
	for _, proto := range []int{multiaddr.P_IP4, multiaddr.P_IP6} {
		raw, err := ma.ValueForProtocol(proto)
		if err != nil {
			continue
		}

		// Unmap so a 4-in-6 form compares equal to the plain v4 one it names.
		if addr, err := netip.ParseAddr(raw); err == nil {
			return addr.Unmap(), true
		}
	}

	return netip.Addr{}, false
}
