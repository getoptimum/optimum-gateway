package udpsession

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/mump2p-protocol/pkg/transport/datagram"
)

func validMessage(t *testing.T) *message {
	t.Helper()

	return &message{
		Version:   Version,
		EphPubKey: mustRandom(t, x25519KeySize),
		Salt:      mustRandom(t, saltSize),
		KeyID:     0x0BADC0DE,
		PathToken: mustRandom(t, datagram.PathTokenSize),
		Endpoints: []string{"127.0.0.1:4101", "[::1]:4101"},
	}
}

func encoded(t *testing.T, m *message) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, writeMessage(&buf, m))

	return buf.Bytes()
}

func TestMessageRoundTrip(t *testing.T) {
	sent := validMessage(t)

	got, err := readMessage(bytes.NewReader(encoded(t, sent)))
	require.NoError(t, err)

	require.Equal(t, sent.Version, got.Version)
	require.Equal(t, sent.EphPubKey, got.EphPubKey)
	require.Equal(t, sent.Salt, got.Salt)
	require.Equal(t, sent.KeyID, got.KeyID)
	require.Equal(t, sent.PathToken, got.PathToken)
	require.Len(t, got.candidates, 2)
	require.Equal(t, datagram.PathToken(sent.PathToken), got.pathToken())
}

// TestOversizedMessageRejected proves the decode is bounded: a peer that is
// authenticated but not trusted cannot grow the decoder's buffer at will.
func TestOversizedMessageRejected(t *testing.T) {
	huge := validMessage(t)
	// Padding in an unknown field, so the payload is otherwise a valid message
	// and only its size can be what rejects it.
	raw, err := json.Marshal(struct {
		*message
		Padding string `json:"padding"`
	}{message: huge, Padding: strings.Repeat("A", 1<<20)})
	require.NoError(t, err)
	require.Greater(t, len(raw), maxMessageBytes)

	_, err = readMessage(bytes.NewReader(raw))
	require.ErrorIs(t, err, ErrTooLarge)
}

// TestOversizeIsDistinguishedFromMalformed pins the slack byte: truncation and
// plain garbage must not report the same way, or an operator triaging a rejected
// peer cannot tell a size attack from a version skew.
func TestOversizeIsDistinguishedFromMalformed(t *testing.T) {
	_, err := readMessage(bytes.NewBufferString("{"))
	require.ErrorIs(t, err, ErrMalformed)
	require.NotErrorIs(t, err, ErrTooLarge)
}

func TestMessageValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(*message)
		wantErr error
	}{
		{
			name:    "WrongVersion",
			mutate:  func(m *message) { m.Version = Version + 1 },
			wantErr: ErrVersion,
		},
		{
			name:    "ShortEphemeralKey",
			mutate:  func(m *message) { m.EphPubKey = m.EphPubKey[:x25519KeySize-1] },
			wantErr: ErrMalformed,
		},
		{
			name:    "ShortSalt",
			mutate:  func(m *message) { m.Salt = m.Salt[:saltSize-1] },
			wantErr: ErrMalformed,
		},
		{
			// Zero is what the datagram layer means by "no key", so a peer asking
			// for it would install a key that can never be selected.
			name:    "ZeroKeyID",
			mutate:  func(m *message) { m.KeyID = 0 },
			wantErr: ErrMalformed,
		},
		{
			name:    "ShortPathToken",
			mutate:  func(m *message) { m.PathToken = m.PathToken[:datagram.PathTokenSize-1] },
			wantErr: ErrMalformed,
		},
		{
			name: "TooManyEndpoints",
			mutate: func(m *message) {
				m.Endpoints = make([]string, maxEndpoints+1)
				for i := range m.Endpoints {
					m.Endpoints[i] = "127.0.0.1:4101"
				}
			},
			wantErr: ErrMalformed,
		},
		{
			name:    "UnparseableEndpoint",
			mutate:  func(m *message) { m.Endpoints = []string{"not-an-address"} },
			wantErr: ErrMalformed,
		},
		{
			name:    "HostnameEndpoint",
			mutate:  func(m *message) { m.Endpoints = []string{"example.com:4101"} },
			wantErr: ErrMalformed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := validMessage(t)
			tc.mutate(m)

			_, err := readMessage(bytes.NewReader(encoded(t, m)))
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// TestEndpointsAreCanonicalized proves a 4-in-6 advertised address compares
// equal to the plain v4 form the socket will report it as.
func TestEndpointsAreCanonicalized(t *testing.T) {
	m := validMessage(t)
	m.Endpoints = []string{"[::ffff:127.0.0.1]:4101"}

	got, err := readMessage(bytes.NewReader(encoded(t, m)))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:4101", got.candidates[0].String())
}
