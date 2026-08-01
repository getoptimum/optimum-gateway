package udpsession

import (
	"net"
	"testing"

	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

func mustMultiaddrs(t *testing.T, raw ...string) []multiaddr.Multiaddr {
	t.Helper()

	out := make([]multiaddr.Multiaddr, 0, len(raw))
	for _, s := range raw {
		ma, err := multiaddr.NewMultiaddr(s)
		require.NoError(t, err)
		out = append(out, ma)
	}

	return out
}

func TestLocalCandidates(t *testing.T) {
	t.Parallel()

	advertised := mustMultiaddrs(t,
		"/ip4/203.0.113.5/udp/33213/quic-v1",
		"/ip6/2001:db8::1/udp/33213/quic-v1",
		// Same IP on another transport must not produce a duplicate endpoint.
		"/ip4/203.0.113.5/tcp/33212",
		"/ip4/0.0.0.0/udp/33213/quic-v1",
	)

	cases := []struct {
		name  string
		local net.Addr
		want  []string
	}{
		{
			name:  "BoundAddressWins",
			local: &net.UDPAddr{IP: net.ParseIP("198.51.100.7"), Port: 4101},
			want:  []string{"198.51.100.7:4101"},
		},
		{
			// Bound to the unspecified address, so the advertised IPs supply the
			// answer and the socket supplies the port.
			name:  "UnspecifiedTakesAdvertisedIPs",
			local: &net.UDPAddr{IP: net.IPv4zero, Port: 4101},
			want:  []string{"203.0.113.5:4101", "[2001:db8::1]:4101"},
		},
		{
			name:  "UnspecifiedV6TakesAdvertisedIPs",
			local: &net.UDPAddr{IP: net.IPv6unspecified, Port: 4101},
			want:  []string{"203.0.113.5:4101", "[2001:db8::1]:4101"},
		},
		{
			// A 4-in-6 bound address names a v4 endpoint, and must compare equal to
			// the plain v4 form the peer will see the datagram arrive from.
			name:  "MappedV4IsUnmapped",
			local: &net.UDPAddr{IP: net.ParseIP("::ffff:198.51.100.7"), Port: 4101},
			want:  []string{"198.51.100.7:4101"},
		},
		{
			// Port zero means the socket is not bound to anything a peer could
			// reach, so advertising it would only earn wasted probes.
			name:  "UnboundPortAdvertisesNothing",
			local: &net.UDPAddr{IP: net.ParseIP("198.51.100.7"), Port: 0},
			want:  nil,
		},
		{
			name:  "NonUDPAddressAdvertisesNothing",
			local: &net.TCPAddr{IP: net.ParseIP("198.51.100.7"), Port: 4101},
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := LocalCandidates(tc.local, advertised)

			out := make([]string, 0, len(got))
			for _, ep := range got {
				out = append(out, ep.String())
			}

			require.Equal(t, tc.want, nilIfEmpty(out))
		})
	}
}

// TestLocalCandidatesIsCapped proves the advertised list cannot grow past what
// one message may carry.
func TestLocalCandidatesIsCapped(t *testing.T) {
	raw := make([]string, 0, maxEndpoints+4)
	for i := range maxEndpoints + 4 {
		raw = append(raw, "/ip4/203.0.113."+string(rune('0'+i%10))+"/udp/33213/quic-v1")
	}

	got := LocalCandidates(&net.UDPAddr{IP: net.IPv4zero, Port: 4101}, mustMultiaddrs(t, raw...))
	require.LessOrEqual(t, len(got), maxEndpoints)
}

// TestLocalCandidatesSkipsNonIPAdvertisements proves a relay or DNS multiaddr,
// which names no IP, is ignored rather than guessed at.
func TestLocalCandidatesSkipsNonIPAdvertisements(t *testing.T) {
	got := LocalCandidates(
		&net.UDPAddr{IP: net.IPv4zero, Port: 4101},
		mustMultiaddrs(t, "/dns4/gateway.example.com/tcp/33212"),
	)
	require.Empty(t, got)
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}

	return s
}
