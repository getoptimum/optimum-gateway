package stream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// newWSTestServer wires a real Server behind httptest so tests exercise the full
// upgrade + auth path. requireAuth=false uses the allow-all (loopback) backend.
func newWSTestServer(t *testing.T, cfg Config, requireAuth bool) (ts *httptest.Server, s *Server, hub *streamhub.Service, rig *test_utils.AuthTestRig) {
	t.Helper()
	rig = test_utils.NewAuthTestRig(t)
	var authenticator ConsumerAuthenticator
	if requireAuth {
		m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
		require.NoError(t, err)
		authenticator = NewConsumerAuthenticator(m, true)
	} else {
		authenticator = NewConsumerAuthenticator(nil, false)
	}
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 256
	}
	if cfg.MaxConnsPerSub == 0 {
		cfg.MaxConnsPerSub = 8
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 64
	}
	hub = streamhub.New()
	s = NewServer(hub, authenticator, cfg, logger.NewAppSLogger(logger.Debug))
	ts = httptest.NewServer(s.httpSrv.Handler)
	t.Cleanup(ts.Close)
	return ts, s, hub, rig
}

func streamToken(t *testing.T, rig *test_utils.AuthTestRig, subject string) string {
	t.Helper()
	return rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
		c.Audience = jwt.ClaimStrings{jwks_verifier.AudStream}
		c.Subject = subject
	})
}

func wsURL(ts *httptest.Server, query string) string {
	return "ws" + strings.TrimPrefix(ts.URL, "http") + streamPath + query
}

// dial returns the handshake status code (not the response) so callers never
// hold an unclosed body; that code is all the reject-path assertions need.
func dial(ts *httptest.Server, query, token string) (*websocket.Conn, int, error) {
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(ts, query), h)
	return conn, closeBody(resp), err
}

// closeBody releases the handshake response body and returns its status code.
func closeBody(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func sampleEvent() *streamhub.BlockEvent {
	return &streamhub.BlockEvent{
		Slot:          42,
		ProposerIndex: 7,
		Topic:         defaultTopic,
		Source:        entities.SourceLibP2P,
		Raw:           []byte("ssz-snappy-bytes"),
	}
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := conn.ReadMessage()
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}

func waitSubscribed(t *testing.T, hub *streamhub.Service, n int) {
	t.Helper()
	require.Eventually(t, func() bool { return hub.SubscriberCount() == n }, time.Second, 5*time.Millisecond)
}

func TestWS_RejectsBeforeUpgrade(t *testing.T) {
	ts, _, hub, _ := newWSTestServer(t, Config{}, true)
	for _, token := range []string{"not-a-jwt", ""} {
		conn, code, err := dial(ts, "", token)
		require.Equal(t, websocket.ErrBadHandshake, err)
		require.Equal(t, http.StatusUnauthorized, code)
		require.Nil(t, conn)
		require.Zero(t, hub.SubscriberCount(), "rejected consumer must not create a subscriber")
	}
}

func TestWS_BlockFraming(t *testing.T) {
	for _, tc := range []struct {
		name      string
		query     string
		expectRaw bool
	}{
		{"metadata omits raw", "?mode=metadata", false},
		{"raw includes bytes", "?mode=raw", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts, _, hub, rig := newWSTestServer(t, Config{}, true)
			conn, _, err := dial(ts, tc.query, streamToken(t, rig, "sub-1"))
			require.NoError(t, err)
			defer conn.Close()

			waitSubscribed(t, hub, 1)
			hub.Emit(sampleEvent())

			f := readFrame(t, conn)
			require.Equal(t, frameTypeBlock, f["type"])
			require.EqualValues(t, 42, f["slot"])
			raw, hasRaw := f["raw"]
			require.Equal(t, tc.expectRaw, hasRaw)
			if tc.expectRaw {
				require.Equal(t, "c3N6LXNuYXBweS1ieXRlcw==", raw)
			}
		})
	}
}

func TestWS_LaggedOnOverflow(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, Config{BufferSize: 1}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	// Emit is non-blocking, so this fills the depth-1 buffer and drops the rest.
	for range 3000 {
		hub.Emit(sampleEvent())
	}

	var sawLagged bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawLagged {
		f := readFrame(t, conn)
		if f["type"] == frameTypeLagged {
			require.Positive(t, f["dropped"])
			sawLagged = true
		}
	}
	require.True(t, sawLagged, "a lagged frame must be sent after overflow")
}

func TestWS_ConnectionCaps(t *testing.T) {
	t.Run("global", func(t *testing.T) {
		ts, _, _, rig := newWSTestServer(t, Config{MaxConns: 1}, true)
		c1, _, err := dial(ts, "", streamToken(t, rig, "sub-a"))
		require.NoError(t, err)
		defer c1.Close()

		_, code, err := dial(ts, "", streamToken(t, rig, "sub-b"))
		require.Error(t, err)
		require.Equal(t, http.StatusServiceUnavailable, code)
	})

	t.Run("per subject", func(t *testing.T) {
		ts, _, _, rig := newWSTestServer(t, Config{MaxConnsPerSub: 1}, true)
		c1, _, err := dial(ts, "", streamToken(t, rig, "same"))
		require.NoError(t, err)
		defer c1.Close()

		_, code, err := dial(ts, "", streamToken(t, rig, "same"))
		require.Error(t, err)
		require.Equal(t, http.StatusServiceUnavailable, code)

		// A different subject is still admitted under the global cap.
		c2, _, err := dial(ts, "", streamToken(t, rig, "other"))
		require.NoError(t, err)
		defer c2.Close()
	})
}

func TestWS_LoopbackNoAuthAccepts(t *testing.T) {
	ts, _, hub, _ := newWSTestServer(t, Config{}, false)
	conn, _, err := dial(ts, "", "")
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	hub.Emit(sampleEvent())
	require.Equal(t, frameTypeBlock, readFrame(t, conn)["type"])
}

func TestWS_SubprotocolTokenNegotiatesMarkerOnly(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, Config{}, true)
	tok := streamToken(t, rig, "sub-1")

	d := websocket.Dialer{Subprotocols: []string{wsSubprotocol, bearerSubproto + tok}}
	conn, resp, err := d.Dial(wsURL(ts, ""), nil)
	closeBody(resp)
	require.NoError(t, err)
	defer conn.Close()

	// Server must select only the marker, never echo the token.
	require.Equal(t, wsSubprotocol, resp.Header.Get("Sec-WebSocket-Protocol"))
	require.Equal(t, wsSubprotocol, conn.Subprotocol())
	waitSubscribed(t, hub, 1)
}

func TestWS_CleanupOnClose(t *testing.T) {
	ts, s, hub, rig := newWSTestServer(t, Config{}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)

	waitSubscribed(t, hub, 1)
	require.NoError(t, conn.Close())

	// Close must unregister the subscriber (drop-counter entry included) and
	// release the cap slot, so nothing leaks.
	waitSubscribed(t, hub, 0)
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.conns == 0 && len(s.perSub) == 0
	}, 2*time.Second, 10*time.Millisecond)
}
