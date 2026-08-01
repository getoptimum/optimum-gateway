package udpsession

import (
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

// agreedKeys runs both halves of the key schedule and returns what each side
// ends up holding.
func agreedKeys(t *testing.T, initiator, responder peer.ID) (initiatorKeys, responderKeys *sessionKeys) {
	t.Helper()

	initEph, err := newEphemeral()
	require.NoError(t, err)
	respEph, err := newEphemeral()
	require.NoError(t, err)

	initPub := initEph.publicKey()
	respPub := respEph.publicKey()

	initSalt := mustRandom(t, saltSize)
	respSalt := mustRandom(t, saltSize)

	initiatorKeys, err = derive(initEph, respPub, initSalt, respSalt, initiator, responder)
	require.NoError(t, err)

	responderKeys, err = derive(respEph, initPub, initSalt, respSalt, initiator, responder)
	require.NoError(t, err)

	return initiatorKeys, responderKeys
}

func mustRandom(t *testing.T, n int) []byte {
	t.Helper()

	b := make([]byte, n)
	_, err := rand.Read(b)
	require.NoError(t, err)

	return b
}

func mustPeerIDs(t *testing.T, n int) []peer.ID {
	t.Helper()

	out := make([]peer.ID, 0, n)
	for range n {
		id, err := peer.IDFromBytes(append([]byte{0x00, 0x20}, mustRandom(t, 32)...))
		require.NoError(t, err)
		out = append(out, id)
	}

	return out
}

// TestKeyScheduleAgrees is the control for the separation tests below: without
// it, a test that proves two keys differ would pass on a schedule that agrees on
// nothing at all.
func TestKeyScheduleAgrees(t *testing.T) {
	ids := mustPeerIDs(t, 2)
	initiatorKeys, responderKeys := agreedKeys(t, ids[0], ids[1])

	require.Equal(t, initiatorKeys.send(true), responderKeys.receive(false),
		"what the initiator seals with must be what the responder opens with")
	require.Equal(t, initiatorKeys.receive(true), responderKeys.send(false),
		"what the responder seals with must be what the initiator opens with")
}

// TestDirectionalKeySeparation proves a peer's send key cannot open its own
// receive direction: a datagram sealed with the key one side sends with must
// fail against the key that same side receives with, so a captured datagram
// cannot be reflected back at its sender.
func TestDirectionalKeySeparation(t *testing.T) {
	ids := mustPeerIDs(t, 2)
	initiatorKeys, _ := agreedKeys(t, ids[0], ids[1])

	send := initiatorKeys.send(true)
	receive := initiatorKeys.receive(true)
	require.NotEqual(t, send, receive, "the two directions must not share a key")

	const keyID = uint32(0x51D0)

	tx, err := datagram.NewTxKey(keyID, append([]byte(nil), send...))
	require.NoError(t, err)

	dgram, err := tx.Seal(nil, datagram.TypeSymbol, []byte("directional payload"))
	require.NoError(t, err)

	// Same key id, so the ring selects this key and the failure is the AEAD's,
	// not a lookup miss that would prove nothing about the key material.
	wrong := datagram.NewKeyRing()
	wrongKey, err := datagram.NewRxKey(keyID, append([]byte(nil), receive...))
	require.NoError(t, err)
	require.NoError(t, wrong.Add(wrongKey))

	_, err = wrong.Open(nil, dgram)
	require.Error(t, err, "the send key must not open what the same side receives")

	right := datagram.NewKeyRing()
	rightKey, err := datagram.NewRxKey(keyID, append([]byte(nil), send...))
	require.NoError(t, err)
	require.NoError(t, right.Add(rightKey))

	opened, err := right.Open(nil, dgram)
	require.NoError(t, err)
	require.Equal(t, []byte("directional payload"), opened.Body)
}

// TestKeyScheduleBindsBothPeerIDs proves the channel binding: the same ECDH
// exchange and the same salts derive different keys for a different pair of
// identities, and for the same pair in swapped roles.
func TestKeyScheduleBindsBothPeerIDs(t *testing.T) {
	ids := mustPeerIDs(t, 3)

	initEph, err := newEphemeral()
	require.NoError(t, err)
	respEph, err := newEphemeral()
	require.NoError(t, err)

	respPub := respEph.publicKey()
	initSalt := mustRandom(t, saltSize)
	respSalt := mustRandom(t, saltSize)

	bound, err := derive(initEph, respPub, initSalt, respSalt, ids[0], ids[1])
	require.NoError(t, err)

	// Re-derive from a fresh ephemeral pair is not comparable, so the binding is
	// checked at the info string, which is the only input the peer IDs reach.
	base, err := bindingInfo(ids[0], ids[1])
	require.NoError(t, err)

	other, err := bindingInfo(ids[0], ids[2])
	require.NoError(t, err)
	require.NotEqual(t, base, other, "a different responder must not produce the same info")

	swapped, err := bindingInfo(ids[1], ids[0])
	require.NoError(t, err)
	require.NotEqual(t, base, swapped, "swapping the roles must not produce the same info")

	require.NotEmpty(t, bound.initiatorToResponder)
}

// TestBindingInfoIsUnambiguous proves the length prefixes do their job: no two
// distinct identity pairs can concatenate to the same info string.
func TestBindingInfoIsUnambiguous(t *testing.T) {
	left, err := bindingInfo(peer.ID("ab"), peer.ID("c"))
	require.NoError(t, err)

	right, err := bindingInfo(peer.ID("a"), peer.ID("bc"))
	require.NoError(t, err)

	require.NotEqual(t, left, right)
}

// TestEphemeralZeroizedAfterECDH proves the private seed is wiped as soon as the
// shared secret exists, rather than left for the collector.
func TestEphemeralZeroizedAfterECDH(t *testing.T) {
	ids := mustPeerIDs(t, 2)

	eph, err := newEphemeral()
	require.NoError(t, err)
	require.NotEqual(t, make([]byte, x25519KeySize), eph.seed)

	peerEph, err := newEphemeral()
	require.NoError(t, err)

	_, err = derive(eph, peerEph.publicKey(), mustRandom(t, saltSize), mustRandom(t, saltSize), ids[0], ids[1])
	require.NoError(t, err)

	require.Equal(t, make([]byte, x25519KeySize), eph.seed, "the ephemeral seed must be wiped")
	require.Nil(t, eph.priv)
}

// TestDeriveRejectsMalformedRemoteKey proves a peer cannot steer the exchange
// with a key the curve does not accept.
func TestDeriveRejectsMalformedRemoteKey(t *testing.T) {
	ids := mustPeerIDs(t, 2)

	eph, err := newEphemeral()
	require.NoError(t, err)

	_, err = derive(eph, make([]byte, 8), mustRandom(t, saltSize), mustRandom(t, saltSize), ids[0], ids[1])
	require.Error(t, err)
}

// TestInitiatorRuleIsTotalAndAsymmetric proves the glare rule: for any two
// distinct peers exactly one side dials, and a peer never dials itself.
func TestInitiatorRuleIsTotalAndAsymmetric(t *testing.T) {
	ids := mustPeerIDs(t, 16)

	for i, left := range ids {
		require.False(t, Initiator(left, left), "a node must not dial itself")

		for _, right := range ids[i+1:] {
			require.NotEqual(t, Initiator(left, right), Initiator(right, left),
				"exactly one of the two sides must dial")
		}
	}
}
