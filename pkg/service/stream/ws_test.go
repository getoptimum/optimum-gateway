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

// testAuth builds the consumer authenticator used by both WS and gRPC tests.
// requireAuth=false uses the allow-all (loopback) backend.
func testAuth(t *testing.T, requireAuth bool) (ConsumerAuthenticator, *test_utils.AuthTestRig) {
	t.Helper()
	rig := test_utils.NewAuthTestRig(t)
	if !requireAuth {
		return NewConsumerAuthenticator(nil, false), rig
	}
	m, err := auth_token.New(t.Context(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	return NewConsumerAuthenticator(m, true), rig
}

func newWSTestServer(t *testing.T, cfg *Config, requireAuth bool) (ts *httptest.Server, s *Server, hub *streamhub.Service, rig *test_utils.AuthTestRig) {
	t.Helper()
	var authenticator ConsumerAuthenticator
	authenticator, rig = testAuth(t, requireAuth)
	hub = streamhub.New()
	s = NewServer(hub, authenticator, cfg, logger.NewAppSLogger(logger.Debug))
	ts = httptest.NewServer(s.httpSrv.Handler)
	t.Cleanup(ts.Close)
	return ts, s, hub, rig
}

// newWSTestServerWithAuth is the same rig with a caller-supplied
// authenticator, for the re-auth modes.
func newWSTestServerWithAuth(t *testing.T, cfg *Config, authenticator ConsumerAuthenticator) (*httptest.Server, *streamhub.Service) {
	t.Helper()
	hub := streamhub.New()
	s := NewServer(hub, authenticator, cfg, logger.NewAppSLogger(logger.Debug))
	ts := httptest.NewServer(s.httpSrv.Handler)
	t.Cleanup(ts.Close)
	return ts, hub
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
	ts, _, hub, _ := newWSTestServer(t, &Config{}, true)
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
			ts, _, hub, rig := newWSTestServer(t, &Config{}, true)
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
	ts, _, hub, rig := newWSTestServer(t, &Config{BufferSize: 1}, true)
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
		ts, _, _, rig := newWSTestServer(t, &Config{MaxConns: 1}, true)
		c1, _, err := dial(ts, "", streamToken(t, rig, "sub-a"))
		require.NoError(t, err)
		defer c1.Close()

		_, code, err := dial(ts, "", streamToken(t, rig, "sub-b"))
		require.Error(t, err)
		require.Equal(t, http.StatusServiceUnavailable, code)
	})

	t.Run("per subject", func(t *testing.T) {
		ts, _, _, rig := newWSTestServer(t, &Config{MaxConnsPerSub: 1}, true)
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
	ts, _, hub, _ := newWSTestServer(t, &Config{}, false)
	conn, _, err := dial(ts, "", "")
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	hub.Emit(sampleEvent())
	require.Equal(t, frameTypeBlock, readFrame(t, conn)["type"])
}

func TestWS_SubprotocolTokenNegotiatesMarkerOnly(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, &Config{}, true)
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
	ts, s, hub, rig := newWSTestServer(t, &Config{}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)

	waitSubscribed(t, hub, 1)
	require.NoError(t, conn.Close())

	// Close must unregister the subscriber (drop-counter entry included) and
	// release the cap slot, so nothing leaks.
	waitSubscribed(t, hub, 0)
	require.Eventually(t, func() bool {
		s.limiter.mu.Lock()
		defer s.limiter.mu.Unlock()
		return s.limiter.conns == 0 && len(s.limiter.perSub) == 0
	}, 2*time.Second, 10*time.Millisecond)
}

// readFrameUntil returns the first frame of the given type, so a test can
// assert on one kind without depending on what precedes it.
func readFrameUntil(t *testing.T, conn *websocket.Conn, frameType string) map[string]any {
	t.Helper()
	return readFrameUntilFunc(t, conn, func(m map[string]any) bool { return m["type"] == frameType })
}

// readFrameUntilFunc is readFrameUntil for a predicate over the whole frame.
func readFrameUntilFunc(t *testing.T, conn *websocket.Conn, want func(map[string]any) bool) map[string]any {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if f := readFrame(t, conn); want(f) {
			return f
		}
	}
	t.Fatal("no matching frame before deadline")
	return nil
}

// TestWS_HeartbeatDuringSilence covers why the WS heartbeat is a data frame and
// not the existing control ping: browsers cannot observe WebSocket ping/pong.
func TestWS_HeartbeatDuringSilence(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)

	f := readFrameUntil(t, conn, frameTypeHeartbeat)
	require.Zero(t, f["last_slot"], "no block has been delivered yet")
	require.Zero(t, f["silence_ms"], "silence is unmeasurable before the first block")
	require.Positive(t, f["expected_slot"], "expected_slot comes from the wall clock, not the feed")
}

func TestWS_HeartbeatReportsLastDeliveredSlot(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	hub.Emit(sampleEvent())

	require.EqualValues(t, 42, readFrameUntil(t, conn, frameTypeBlock)["slot"])
	// Wait for silence to become measurable: a regression that set lastSlot
	// without lastBlock would report 0 forever and still pass.
	f := readFrameUntilFunc(t, conn, func(m map[string]any) bool {
		v, ok := m["silence_ms"].(float64)
		return m["type"] == frameTypeHeartbeat && ok && v > 0
	})
	require.EqualValues(t, 42, f["last_slot"], "the heartbeat must report the last slot actually written")
	require.Greater(t, f["expected_slot"], f["last_slot"], "sampleEvent is a historical slot, so the feed reads as behind")
}

// TestWS_IgnoresNonRefreshMessages: consumers are read-only apart from a token
// refresh, so anything else they send must not disturb the connection.
func TestWS_IgnoresNonRefreshMessages(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("not json at all")))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"unrelated":true}`)))

	hub.Emit(sampleEvent())
	require.EqualValues(t, 42, readFrameUntil(t, conn, frameTypeBlock)["slot"])
	require.Equal(t, 1, hub.SubscriberCount())
}

func TestWS_AcceptsRefreshedTokenMidStream(t *testing.T) {
	ts, _, hub, rig := newWSTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond}, true)
	conn, _, err := dial(ts, "", streamToken(t, rig, "sub-1"))
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	refresh, err := json.Marshal(map[string]string{"token": streamToken(t, rig, "sub-1")})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, refresh))

	require.NotNil(t, readFrameUntil(t, conn, frameTypeHeartbeat))
	hub.Emit(sampleEvent())
	require.EqualValues(t, 42, readFrameUntil(t, conn, frameTypeBlock)["slot"])
	require.Equal(t, 1, hub.SubscriberCount(), "a refresh must not churn the subscription")
}

// TestWS_ReauthEnforceClosesWithReason covers the only close frame this
// transport sends; the code is all that distinguishes an auth cut from a drop.
func TestWS_ReauthEnforceClosesWithReason(t *testing.T) {
	auth := &revocableAuth{}
	ts, hub := newWSTestServerWithAuth(t, &Config{
		ReauthMode: ReauthEnforce, ReauthInterval: 20 * time.Millisecond,
	}, auth)
	conn, _, err := dial(ts, "", "any-token")
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	auth.revoked.Store(true)

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, rerr := conn.ReadMessage(); rerr != nil {
			require.True(t, websocket.IsCloseError(rerr, websocket.ClosePolicyViolation),
				"expected a 1008 close, got %v", rerr)
			require.ErrorContains(t, rerr, "token expired, refresh required")
			break
		}
	}
	waitSubscribed(t, hub, 0)
}

func TestWS_ReauthObserveKeepsStream(t *testing.T) {
	auth := &revocableAuth{}
	ts, hub := newWSTestServerWithAuth(t, &Config{
		ReauthMode: ReauthObserve, ReauthInterval: 20 * time.Millisecond,
		HeartbeatInterval: 20 * time.Millisecond,
	}, auth)
	conn, _, err := dial(ts, "", "any-token")
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	auth.revoked.Store(true)

	require.NotNil(t, readFrameUntil(t, conn, frameTypeHeartbeat))
	hub.Emit(sampleEvent())
	require.EqualValues(t, 42, readFrameUntil(t, conn, frameTypeBlock)["slot"])
	require.Equal(t, 1, hub.SubscriberCount())
}

// TestWS_RefreshRejectsInvalidToken reaches the writer's Authenticate failure
// branch: a token that does not verify is counted, dropped, and not fatal.
func TestWS_RefreshRejectsInvalidToken(t *testing.T) {
	auth := newSubjectAuth(map[string]string{"tok-1": testSubject})
	ts, hub := newWSTestServerWithAuth(t, &Config{HeartbeatInterval: 20 * time.Millisecond}, auth)
	conn, _, err := dial(ts, "", "tok-1")
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	bad, err := json.Marshal(map[string]string{"token": "not-a-token"})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, bad))

	// WriteMessage does not wait for the read pump, so the authentication
	// attempt is the only proof the branch ran.
	require.Eventually(t, func() bool { return auth.authenticated("not-a-token") > 0 },
		3*time.Second, 5*time.Millisecond, "the refresh was never read")

	hub.Emit(sampleEvent())
	require.EqualValues(t, 42, readFrameUntil(t, conn, frameTypeBlock)["slot"])
	require.Equal(t, 1, hub.SubscriberCount(), "a bad refresh must not sever a working stream")
}

// TestWS_RefreshAcceptsALargeToken is the read limit's boundary: at the old
// 1 KiB an envelope this size closed the connection instead of refreshing.
func TestWS_RefreshAcceptsALargeToken(t *testing.T) {
	big := "tok-" + strings.Repeat("x", 1400)
	auth := newSubjectAuth(map[string]string{"tok-1": testSubject, big: testSubject})
	ts, hub := newWSTestServerWithAuth(t, &Config{HeartbeatInterval: 20 * time.Millisecond}, auth)
	conn, _, err := dial(ts, "", "tok-1")
	require.NoError(t, err)
	defer conn.Close()

	waitSubscribed(t, hub, 1)
	env, err := json.Marshal(map[string]string{"token": big})
	require.NoError(t, err)
	require.Greater(t, len(env), 1<<10, "the point of the test is an envelope past the old limit")
	require.LessOrEqual(t, int64(len(env)), int64(maxReadBytes))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, env))

	// Under the old limit the connection would be closed instead, so this never
	// becomes true.
	require.Eventually(t, func() bool { return auth.authenticated(big) > 0 },
		3*time.Second, 5*time.Millisecond, "the large envelope was never read")

	hub.Emit(sampleEvent())
	require.EqualValues(t, 42, readFrameUntil(t, conn, frameTypeBlock)["slot"],
		"the connection must survive a large but valid refresh")
	require.Equal(t, 1, hub.SubscriberCount())
}
