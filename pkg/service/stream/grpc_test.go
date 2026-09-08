package stream

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/getoptimum/optimum-common/pkg/logger"
	streamv1 "github.com/getoptimum/optimum-gateway/pkg/service/stream/v1"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// newGRPCTestServer starts a GRPCServer over an in-memory bufconn and returns a
// connected client. Auth is always required; loopback no-auth is covered by WS.
func newGRPCTestServer(t *testing.T, cfg *Config) (client streamv1.BlockStreamServiceClient, srv *GRPCServer, hub *streamhub.Service, rig *test_utils.AuthTestRig) {
	t.Helper()
	var authenticator ConsumerAuthenticator
	authenticator, rig = testAuth(t, true)
	client, srv, hub = newGRPCTestServerWithAuth(t, cfg, authenticator)
	return client, srv, hub, rig
}

// newGRPCTestServerWithAuth is the same rig with a caller-supplied
// authenticator, for the re-auth modes: they need authentication to start
// succeeding and then fail, which no real token can be made to do quickly
// because the verifier allows 30s of clock skew.
func newGRPCTestServerWithAuth(t *testing.T, cfg *Config, authenticator ConsumerAuthenticator) (streamv1.BlockStreamServiceClient, *GRPCServer, *streamhub.Service) {
	t.Helper()
	hub := streamhub.New()
	srv := NewGRPCServer(hub, authenticator, cfg, logger.NewAppSLogger(logger.Debug))

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.grpcSrv.Serve(lis) }()
	t.Cleanup(srv.grpcSrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return streamv1.NewBlockStreamServiceClient(conn), srv, hub
}

func authCtx(t *testing.T, rig *test_utils.AuthTestRig, subject string) context.Context {
	t.Helper()
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+streamToken(t, rig, subject))
}

// subscribeGRPC opens the bidi stream and sends the selection message, which is
// the whole client handshake. Server-side rejections surface on the first Recv,
// not here, because a bidi Subscribe returns without waiting for the server.
func subscribeGRPC(ctx context.Context, t *testing.T, client streamv1.BlockStreamServiceClient, req *streamv1.SubscribeRequest) grpc.BidiStreamingClient[streamv1.SubscribeRequest, streamv1.BlockEvent] {
	t.Helper()
	sub, err := client.Subscribe(ctx)
	require.NoError(t, err)
	require.NoError(t, sub.Send(req))
	return sub
}

// recvUntil returns the first frame satisfying want, so a test can assert on a
// frame type without depending on how many blocks or heartbeats precede it.
func recvUntil(t *testing.T, sub grpc.BidiStreamingClient[streamv1.SubscribeRequest, streamv1.BlockEvent], want func(*streamv1.BlockEvent) bool) *streamv1.BlockEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ev, err := sub.Recv()
		require.NoError(t, err)
		if want(ev) {
			return ev
		}
	}
	t.Fatal("no matching frame before deadline")
	return nil
}

func TestGRPC_RejectsWithoutToken(t *testing.T) {
	client, _, hub, _ := newGRPCTestServer(t, &Config{})

	sub := subscribeGRPC(context.Background(), t, client, &streamv1.SubscribeRequest{})
	_, err := sub.Recv()
	require.Equal(t, codes.Unauthenticated, status.Code(err))
	require.Zero(t, hub.SubscriberCount(), "rejected consumer must not create a subscriber")
}

func TestGRPC_DeliversFraming(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      string
		expectRaw bool
	}{
		{"metadata omits raw", "metadata", false},
		{"raw includes bytes", "raw", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _, hub, rig := newGRPCTestServer(t, &Config{})
			sub := subscribeGRPC(authCtx(t, rig, "sub-1"), t, client, &streamv1.SubscribeRequest{Mode: tc.mode})

			waitSubscribed(t, hub, 1)
			hub.Emit(sampleEvent())

			ev, err := sub.Recv()
			require.NoError(t, err)
			require.Nil(t, ev.GetLagged(), "a block frame carries no lagged signal")
			require.EqualValues(t, 42, ev.GetBlock().GetSlot())
			if tc.expectRaw {
				require.Equal(t, []byte("ssz-snappy-bytes"), ev.GetBlock().GetRaw())
			} else {
				require.Empty(t, ev.GetBlock().GetRaw())
			}
		})
	}
}

func TestGRPC_LaggedOnOverflow(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{BufferSize: 1})
	// Above the loop deadline below, so a blocked Recv fails instead of hanging.
	ctx, cancel := context.WithTimeout(authCtx(t, rig, "sub-1"), 5*time.Second)
	defer cancel()
	sub := subscribeGRPC(ctx, t, client, &streamv1.SubscribeRequest{})

	waitSubscribed(t, hub, 1)
	for range 3000 {
		hub.Emit(sampleEvent())
	}

	var sawLagged bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && !sawLagged {
		ev, rerr := sub.Recv()
		require.NoError(t, rerr)
		if lag := ev.GetLagged(); lag != nil {
			require.Positive(t, lag.GetDropped())
			sawLagged = true
		}
	}
	require.True(t, sawLagged, "a lagged frame must be sent after overflow")
}

func TestGRPC_GlobalCapRejects(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{MaxConns: 1})

	first := subscribeGRPC(authCtx(t, rig, "sub-a"), t, client, &streamv1.SubscribeRequest{})
	waitSubscribed(t, hub, 1)

	second := subscribeGRPC(authCtx(t, rig, "sub-b"), t, client, &streamv1.SubscribeRequest{})
	_, err := second.Recv()
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	_ = first // keep the first stream open for the duration of the assertion
}

func TestGRPC_CleanupOnCancel(t *testing.T) {
	client, srv, hub, rig := newGRPCTestServer(t, &Config{})
	ctx, cancel := context.WithCancel(authCtx(t, rig, "sub-1"))
	subscribeGRPC(ctx, t, client, &streamv1.SubscribeRequest{})

	waitSubscribed(t, hub, 1)
	cancel()

	// Cancel must unwind the handler, closing the subscriber (drop-counter entry
	// included) and releasing the cap slot, so nothing leaks.
	waitSubscribed(t, hub, 0)
	require.Eventually(t, func() bool {
		srv.limiter.mu.Lock()
		defer srv.limiter.mu.Unlock()
		return srv.limiter.conns == 0 && len(srv.limiter.perSub) == 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestGRPC_HeartbeatDuringSilence(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond})
	sub := subscribeGRPC(authCtx(t, rig, "sub-1"), t, client, &streamv1.SubscribeRequest{})
	waitSubscribed(t, hub, 1)

	// No block is ever emitted: this is exactly the starvation the frame exists
	// to make visible, and the only case where nothing else would arrive.
	hb := recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetHeartbeat() != nil }).GetHeartbeat()
	require.Zero(t, hb.GetLastSlot(), "no block has been delivered yet")
	require.Zero(t, hb.GetSilenceMs(), "silence is unmeasurable before the first block")
	require.Positive(t, hb.GetExpectedSlot(), "expected_slot comes from the wall clock, not the feed")
}

func TestGRPC_HeartbeatReportsLastDeliveredSlot(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond})
	sub := subscribeGRPC(authCtx(t, rig, "sub-1"), t, client, &streamv1.SubscribeRequest{})
	waitSubscribed(t, hub, 1)
	hub.Emit(sampleEvent())

	require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetBlock() != nil }))
	hb := recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetHeartbeat() != nil }).GetHeartbeat()
	require.EqualValues(t, 42, hb.GetLastSlot(), "the heartbeat must report the last slot actually written")
	require.Greater(t, hb.GetExpectedSlot(), hb.GetLastSlot(), "sampleEvent is a historical slot, so the feed reads as behind")
}

// TestGRPC_ServerStreamingStubIsStillServed is the wire-compatibility claim
// behind changing Subscribe to bidi: a client generated against the old
// server-streaming signature sends one message and half-closes. CloseSend
// reproduces that on the wire, and the stream must be served as before.
func TestGRPC_ServerStreamingStubIsStillServed(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{})
	sub := subscribeGRPC(authCtx(t, rig, "sub-1"), t, client, &streamv1.SubscribeRequest{})
	require.NoError(t, sub.CloseSend())

	waitSubscribed(t, hub, 1)
	hub.Emit(sampleEvent())

	ev, err := sub.Recv()
	require.NoError(t, err)
	require.EqualValues(t, 42, ev.GetBlock().GetSlot(), "half-close must not end the response stream")
}

func TestGRPC_AcceptsRefreshedTokenMidStream(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond})
	sub := subscribeGRPC(authCtx(t, rig, "sub-1"), t, client, &streamv1.SubscribeRequest{})
	waitSubscribed(t, hub, 1)

	require.NoError(t, sub.Send(&streamv1.SubscribeRequest{Token: streamToken(t, rig, "sub-1")}))

	// The refresh is applied on the send loop, so the proof it did not disturb
	// delivery is that frames keep arriving afterwards.
	require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetHeartbeat() != nil }))
	hub.Emit(sampleEvent())
	require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetBlock() != nil }))
	require.Equal(t, 1, hub.SubscriberCount(), "a refresh must not churn the subscription")
}

// TestGRPC_RefreshRejectsBadTokenWithoutClosing: a garbage refresh is the
// client's bug, not grounds for severing a working stream. It is counted and
// the previous token stays in force.
func TestGRPC_RefreshRejectsBadTokenWithoutClosing(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{HeartbeatInterval: 20 * time.Millisecond})
	sub := subscribeGRPC(authCtx(t, rig, "sub-1"), t, client, &streamv1.SubscribeRequest{})
	waitSubscribed(t, hub, 1)

	require.NoError(t, sub.Send(&streamv1.SubscribeRequest{Token: "not-a-jwt"}))

	hub.Emit(sampleEvent())
	require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetBlock() != nil }))
	require.Equal(t, 1, hub.SubscriberCount())
}

// revocableAuth authenticates until revoke is called. That is what a token
// expiring or being revoked mid-stream looks like to the handler, without
// depending on a clock the verifier deliberately fuzzes by 30s.
type revocableAuth struct{ revoked atomic.Bool }

func (a *revocableAuth) Authenticate(string) (string, error) {
	if a.revoked.Load() {
		return "", errors.New("token no longer valid")
	}
	return "sub-1", nil
}

// TestGRPC_ReauthMode covers the mode switch, which is the point of shipping
// re-auth observe-only first: the same revocation must count in observe and
// close in enforce.
func TestGRPC_ReauthMode(t *testing.T) {
	t.Run("enforce closes the stream", func(t *testing.T) {
		auth := &revocableAuth{}
		client, _, hub := newGRPCTestServerWithAuth(t, &Config{
			ReauthMode: ReauthEnforce, ReauthInterval: 20 * time.Millisecond,
		}, auth)
		sub := subscribeGRPC(context.Background(), t, client, &streamv1.SubscribeRequest{})
		waitSubscribed(t, hub, 1)

		auth.revoked.Store(true)

		var err error
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			if _, err = sub.Recv(); err != nil {
				break
			}
		}
		require.Equal(t, codes.Unauthenticated, status.Code(err), "enforce must close once the token stops verifying")
		waitSubscribed(t, hub, 0)
	})

	t.Run("observe keeps the stream", func(t *testing.T) {
		auth := &revocableAuth{}
		client, _, hub := newGRPCTestServerWithAuth(t, &Config{
			ReauthMode: ReauthObserve, ReauthInterval: 20 * time.Millisecond,
			HeartbeatInterval: 20 * time.Millisecond,
		}, auth)
		sub := subscribeGRPC(context.Background(), t, client, &streamv1.SubscribeRequest{})
		waitSubscribed(t, hub, 1)

		auth.revoked.Store(true)

		// Many re-auth ticks pass, all failing; observe mode only counts them.
		require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetHeartbeat() != nil }))
		hub.Emit(sampleEvent())
		require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetBlock() != nil }))
		require.Equal(t, 1, hub.SubscriberCount())
	})

	t.Run("off never re-verifies", func(t *testing.T) {
		auth := &revocableAuth{}
		client, _, hub := newGRPCTestServerWithAuth(t, &Config{
			ReauthMode: ReauthOff, HeartbeatInterval: 20 * time.Millisecond,
		}, auth)
		sub := subscribeGRPC(context.Background(), t, client, &streamv1.SubscribeRequest{})
		waitSubscribed(t, hub, 1)

		auth.revoked.Store(true)

		hub.Emit(sampleEvent())
		require.NotNil(t, recvUntil(t, sub, func(ev *streamv1.BlockEvent) bool { return ev.GetBlock() != nil }))
		require.Equal(t, 1, hub.SubscriberCount())
	})
}

// TestGRPC_HalfCloseWithoutSelectingIsRejected pins the one case bidi adds that
// server-streaming could not express: a client that opens the stream and closes
// it again without ever sending a selection message.
func TestGRPC_HalfCloseWithoutSelectingIsRejected(t *testing.T) {
	client, _, hub, rig := newGRPCTestServer(t, &Config{})
	sub, err := client.Subscribe(authCtx(t, rig, "sub-1"))
	require.NoError(t, err)
	require.NoError(t, sub.CloseSend())

	_, err = sub.Recv()
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Zero(t, hub.SubscriberCount(), "nothing was selected, so nothing is subscribed")
}

func TestOfferLatest(t *testing.T) {
	ch := make(chan string, 1)

	// Never blocks, and the newest value is the one that survives.
	offerLatest(ch, "first")
	offerLatest(ch, "second")
	offerLatest(ch, "third")
	require.Equal(t, "third", <-ch)
	require.Empty(t, ch, "only one value is ever queued")
}
