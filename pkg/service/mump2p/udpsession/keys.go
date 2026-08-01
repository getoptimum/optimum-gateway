package udpsession

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

// x25519KeySize is the length of an X25519 scalar and of an X25519 public key.
const x25519KeySize = 32

// hkdfLabel domain separates this key schedule from every other HKDF use in the
// system, so material derived here can never collide with material derived
// elsewhere from the same secret.
const hkdfLabel = "optimum/udp-session/v1"

// Direction labels. Each direction gets its own key, so a peer's send key
// cannot open what that peer receives and a captured datagram cannot be
// reflected back at its sender.
const (
	dirInitiatorToResponder = "i2r"
	dirResponderToInitiator = "r2i"
)

// Initiator reports whether local dials remote.
//
// The lower peer ID by byte comparison dials and the higher never does. This
// eliminates simultaneous open rather than resolving it: there is no glare state
// to converge because only one side ever opens.
func Initiator(local, remote peer.ID) bool {
	return local < remote
}

// ephemeral is one side's X25519 key pair for a single establishment.
//
// The seed is held separately from the key so it can be wiped: crypto/ecdh does
// not expose the scalar it derives.
type ephemeral struct {
	priv *ecdh.PrivateKey
	seed []byte
}

func newEphemeral() (*ephemeral, error) {
	seed := make([]byte, x25519KeySize)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("udpsession: read ephemeral seed: %w", err)
	}

	priv, err := ecdh.X25519().NewPrivateKey(seed)
	if err != nil {
		clear(seed)
		return nil, fmt.Errorf("udpsession: derive ephemeral key: %w", err)
	}

	return &ephemeral{priv: priv, seed: seed}, nil
}

// publicKey is what goes on the wire.
func (e *ephemeral) publicKey() []byte {
	return e.priv.PublicKey().Bytes()
}

// zeroize wipes the seed and drops the key.
//
// Best effort only: crypto/ecdh keeps a copy of the scalar it does not expose,
// and the GC may already have moved either, so this shortens the window in
// which the private key is recoverable rather than closing it.
func (e *ephemeral) zeroize() {
	clear(e.seed)
	e.priv = nil
}

// sessionKeys is one establishment's derived material, one key per direction.
type sessionKeys struct {
	initiatorToResponder []byte
	responderToInitiator []byte
}

// derive computes the shared secret and expands it into the two directional
// keys, then wipes everything it no longer needs.
//
// Both peer IDs go into the HKDF info as channel binding: the keys are valid
// only between exactly these two identities in exactly these two roles, so
// material from an exchange with one peer cannot be steered into a session with
// another.
func derive(
	local *ephemeral,
	remotePubKey []byte,
	initiatorSalt, responderSalt []byte,
	initiator, responder peer.ID,
) (*sessionKeys, error) {
	info, err := bindingInfo(initiator, responder)
	if err != nil {
		return nil, err
	}

	remotePub, err := ecdh.X25519().NewPublicKey(remotePubKey)
	if err != nil {
		return nil, fmt.Errorf("udpsession: remote ephemeral key: %w", err)
	}

	shared, err := local.priv.ECDH(remotePub)
	// The private key has served its only purpose, so it goes now rather than at
	// the end of the function or at the mercy of the collector.
	local.zeroize()

	if err != nil {
		return nil, fmt.Errorf("udpsession: x25519: %w", err)
	}
	defer clear(shared)

	// Salt order is by role, not by who sent which message, so both sides build
	// the identical salt without exchanging anything more.
	salt := make([]byte, 0, 2*saltSize)
	salt = append(salt, initiatorSalt...)
	salt = append(salt, responderSalt...)

	i2r, err := hkdf.Key(sha256.New, shared, salt, info+dirInitiatorToResponder, datagram.KeySize)
	if err != nil {
		return nil, fmt.Errorf("udpsession: derive %s key: %w", dirInitiatorToResponder, err)
	}

	r2i, err := hkdf.Key(sha256.New, shared, salt, info+dirResponderToInitiator, datagram.KeySize)
	if err != nil {
		clear(i2r)
		return nil, fmt.Errorf("udpsession: derive %s key: %w", dirResponderToInitiator, err)
	}

	return &sessionKeys{initiatorToResponder: i2r, responderToInitiator: r2i}, nil
}

// send reports the key this side seals with, and receive the key it opens with.
func (k *sessionKeys) send(isInitiator bool) []byte {
	if isInitiator {
		return k.initiatorToResponder
	}
	return k.responderToInitiator
}

func (k *sessionKeys) receive(isInitiator bool) []byte {
	if isInitiator {
		return k.responderToInitiator
	}
	return k.initiatorToResponder
}

// bindingInfo is the HKDF info prefix: the label followed by both peer IDs,
// each length prefixed and in role order, so no identity or role can be shifted
// between the two fields to produce the same info from a different pairing.
func bindingInfo(initiator, responder peer.ID) (string, error) {
	var b strings.Builder

	b.WriteString(hkdfLabel)

	for _, id := range []peer.ID{initiator, responder} {
		if len(id) == 0 || len(id) > math.MaxUint16 {
			return "", fmt.Errorf("%w: peer id length %d is out of range", ErrMalformed, len(id))
		}

		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(len(id))) //nolint:gosec // bounded by the check above
		b.Write(size[:])
		b.WriteString(string(id))
	}

	b.WriteByte('|')

	return b.String(), nil
}
