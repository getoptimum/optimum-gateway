package stream

import (
	"context"
	"net"
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
func newGRPCTestServer(t *testing.T, cfg Config) (client streamv1.BlockStreamServiceClient, srv *GRPCServer, hub *streamhub.Service, rig *test_utils.AuthTestRig) {
	t.Helper()
	var authenticator ConsumerAuthenticator
	authenticator, rig = testAuth(t, true)
	hub = streamhub.New()
	srv = NewGRPCServer(hub, authenticator, cfg, logger.NewAppSLogger(logger.Debug))

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.grpcSrv.Serve(lis) }()
	t.Cleanup(srv.grpcSrv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return streamv1.NewBlockStreamServiceClient(conn), srv, hub, rig
}

func authCtx(t *testing.T, rig *test_utils.AuthTestRig, subject string) context.Context {
	t.Helper()
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+streamToken(t, rig, subject))
}

func TestGRPC_RejectsWithoutToken(t *testing.T) {
	client, _, hub, _ := newGRPCTestServer(t, Config{})

	sub, err := client.Subscribe(context.Background(), &streamv1.SubscribeRequest{})
	require.NoError(t, err)
	_, err = sub.Recv()
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
			client, _, hub, rig := newGRPCTestServer(t, Config{})
			sub, err := client.Subscribe(authCtx(t, rig, "sub-1"), &streamv1.SubscribeRequest{Mode: tc.mode})
			require.NoError(t, err)

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
	client, _, hub, rig := newGRPCTestServer(t, Config{BufferSize: 1})
	// Above the loop deadline below, so a blocked Recv fails instead of hanging.
	ctx, cancel := context.WithTimeout(authCtx(t, rig, "sub-1"), 5*time.Second)
	defer cancel()
	sub, err := client.Subscribe(ctx, &streamv1.SubscribeRequest{})
	require.NoError(t, err)

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
	client, _, hub, rig := newGRPCTestServer(t, Config{MaxConns: 1})

	first, err := client.Subscribe(authCtx(t, rig, "sub-a"), &streamv1.SubscribeRequest{})
	require.NoError(t, err)
	waitSubscribed(t, hub, 1)

	second, err := client.Subscribe(authCtx(t, rig, "sub-b"), &streamv1.SubscribeRequest{})
	require.NoError(t, err)
	_, err = second.Recv()
	require.Equal(t, codes.ResourceExhausted, status.Code(err))

	_ = first // keep the first stream open for the duration of the assertion
}

func TestGRPC_CleanupOnCancel(t *testing.T) {
	client, srv, hub, rig := newGRPCTestServer(t, Config{})
	ctx, cancel := context.WithCancel(authCtx(t, rig, "sub-1"))
	_, err := client.Subscribe(ctx, &streamv1.SubscribeRequest{})
	require.NoError(t, err)

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
