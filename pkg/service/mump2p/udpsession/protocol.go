// Package udpsession establishes the datagram data plane's per-peer keys, key
// ids and path tokens over the libp2p stream that already carries the cluster
// handshake.
//
// The stream is noise authenticated and the connection is JWT gated before this
// protocol runs, so the remote peer ID is cryptographically proven by the
// transport underneath. Nothing here is signed for that reason: an attacker able
// to forge one of these messages could already impersonate the peer at the
// connection layer, where a signature would not help.
package udpsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"

	"github.com/libp2p/go-libp2p/core/protocol"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

const (
	// ProtocolID is versioned in the path. A change to the message shape or to
	// the key schedule is a new protocol rather than a negotiated variant of this
	// one, so an old peer fails to negotiate instead of silently deriving keys it
	// disagrees about.
	ProtocolID = protocol.ID("/optimum/v1/udp-session")

	// Version pins the key schedule. It travels in the payload as well as in the
	// path so a peer that reused the path with a different schedule is rejected
	// before any key material is derived.
	Version uint8 = 1
)

const (
	// saltSize is one side's random contribution to the HKDF salt.
	saltSize = 32

	// maxEndpoints caps the advertised candidate list. The path validator probes
	// at most its own MaxCandidates of them, so a longer list would only buy a
	// peer parsing work at our expense.
	maxEndpoints = 8

	// maxMessageBytes bounds the read before decoding. One message is a version,
	// a 32 byte public key, a 32 byte salt, a key id, a 16 byte token and at most
	// maxEndpoints addresses, which stays well under 1 KiB base64 encoded. 4 KiB
	// leaves headroom while keeping a peer from streaming into the decoder.
	maxMessageBytes = 4 << 10
)

// Errors reported by the session protocol.
var (
	ErrVersion      = errors.New("udpsession: unsupported protocol version")
	ErrMalformed    = errors.New("udpsession: malformed session message")
	ErrTooLarge     = errors.New("udpsession: session message exceeds the size limit")
	ErrNotAdmitted  = errors.New("udpsession: peer is not admitted")
	ErrExpiredToken = errors.New("udpsession: authorizing token has already expired")
	ErrGlare        = errors.New("udpsession: peer dialed against the initiator rule")
	ErrKeyIDSpace   = errors.New("udpsession: could not allocate a free receive key id")
)

// message is one side's half of the exchange. Both directions carry the same
// shape: each side describes what it wants stamped on the datagrams it will
// receive, and where it can be reached.
type message struct {
	// Version is the key schedule this message derives under.
	Version uint8 `json:"version"`

	// EphPubKey is this side's X25519 ephemeral public key.
	EphPubKey []byte `json:"eph_pub_key"`

	// Salt is this side's random HKDF salt contribution.
	Salt []byte `json:"salt"`

	// KeyID is receiver allocated, the IPsec SPI model: the sender picked an id
	// free in its own receive table and is telling the peer to stamp it on every
	// datagram sent to it. Unique by construction, so no collision protocol is
	// needed, and a fresh id per rekey lets two epochs coexist unambiguously.
	KeyID uint32 `json:"key_id"`

	// PathToken is the token this side issues: every probe the peer sends here
	// must carry it.
	PathToken []byte `json:"path_token"`

	// Endpoints are the UDP addresses this side can be reached on. Nothing about
	// them is trusted; each is only somewhere to send one probe.
	Endpoints []string `json:"endpoints"`

	// candidates is Endpoints parsed, filled by validate so the wire strings are
	// parsed exactly once and never reach the transport unvalidated.
	candidates []netip.AddrPort
}

// validate rejects everything the key schedule and the transport rely on being
// well formed, before any of it is used.
func (m *message) validate() error {
	if m.Version != Version {
		return fmt.Errorf("%w: got %d, want %d", ErrVersion, m.Version, Version)
	}

	if len(m.EphPubKey) != x25519KeySize {
		return fmt.Errorf("%w: ephemeral key is %d bytes, want %d", ErrMalformed, len(m.EphPubKey), x25519KeySize)
	}

	if len(m.Salt) != saltSize {
		return fmt.Errorf("%w: salt is %d bytes, want %d", ErrMalformed, len(m.Salt), saltSize)
	}

	// A zero key id is what the datagram layer uses to mean "no key", so a peer
	// asking for one would install a key that can never be selected.
	if m.KeyID == 0 {
		return fmt.Errorf("%w: key id must be non-zero", ErrMalformed)
	}

	if len(m.PathToken) != datagram.PathTokenSize {
		return fmt.Errorf("%w: path token is %d bytes, want %d",
			ErrMalformed, len(m.PathToken), datagram.PathTokenSize)
	}

	if len(m.Endpoints) > maxEndpoints {
		return fmt.Errorf("%w: %d endpoints exceeds the %d limit", ErrMalformed, len(m.Endpoints), maxEndpoints)
	}

	m.candidates = make([]netip.AddrPort, 0, len(m.Endpoints))
	for _, raw := range m.Endpoints {
		ep, err := netip.ParseAddrPort(raw)
		if err != nil {
			return fmt.Errorf("%w: endpoint %q: %w", ErrMalformed, raw, err)
		}
		m.candidates = append(m.candidates, datagram.CanonicalAddrPort(ep))
	}

	return nil
}

// pathToken returns the token as the transport's fixed size type. The length is
// already checked by validate.
func (m *message) pathToken() datagram.PathToken {
	var token datagram.PathToken
	copy(token[:], m.PathToken)
	return token
}

func writeMessage(w io.Writer, m *message) error {
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return fmt.Errorf("udpsession: encode session message: %w", err)
	}
	return nil
}

// readMessage decodes one message from a bounded prefix of r.
//
// The bound is applied before the decoder sees anything: this runs on a
// connection whose peer is authenticated but whose intent is not, and an
// unbounded decode would let it grow the decoder's buffer at will.
func readMessage(r io.Reader) (*message, error) {
	// One byte of slack, so an exhausted N means the peer went over the cap
	// rather than that it sent exactly maxMessageBytes.
	limited := &io.LimitedReader{R: r, N: maxMessageBytes + 1}

	var m message
	if err := json.NewDecoder(limited).Decode(&m); err != nil {
		if limited.N <= 0 {
			// Truncation surfaces as "unexpected EOF", which hides the cause.
			return nil, fmt.Errorf("%w of %d bytes: %w", ErrTooLarge, maxMessageBytes, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}

	if limited.N <= 0 {
		return nil, fmt.Errorf("%w of %d bytes", ErrTooLarge, maxMessageBytes)
	}

	if err := m.validate(); err != nil {
		return nil, err
	}

	return &m, nil
}
