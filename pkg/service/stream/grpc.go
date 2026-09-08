package stream

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/getoptimum/optimum-common/pkg/logger"
	streamv1 "github.com/getoptimum/optimum-gateway/pkg/service/stream/v1"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

// GRPCServer serves the consumer block-stream over gRPC on its own listener,
// reusing the hub, authenticator, and connection caps of the WS transport.
type GRPCServer struct {
	streamv1.UnimplementedBlockStreamServiceServer
	hub     *streamhub.Service
	auth    ConsumerAuthenticator
	cfg     Config
	log     logger.AppLogger
	limiter *ConnLimiter
	grpcSrv *grpc.Server
}

// maxConcurrentStreams bounds Subscribe streams per connection. The caps run
// after auth, so one unauthenticated socket would otherwise be unbounded.
const maxConcurrentStreams = 256

// NewGRPCServer builds the consumer gRPC server. It does not start listening;
// call Run.
func NewGRPCServer(hub *streamhub.Service, auth ConsumerAuthenticator, cfg *Config, log logger.AppLogger) *GRPCServer {
	conf := withDefaults(cfg)
	g := &GRPCServer{
		hub:     hub,
		auth:    auth,
		cfg:     conf,
		log:     log.With(logger.WithService("stream-grpc")),
		limiter: conf.Limiter,
		grpcSrv: grpc.NewServer(
			// Reap dead peers on the WS clock; the gRPC default is a 2h ping.
			grpc.KeepaliveParams(keepalive.ServerParameters{Time: pingPeriod, Timeout: writeWait}),
			// Without this the gRPC-Go defaults apply, MinTime 5m and
			// PermitWithoutStream false, so a consumer that enables client
			// keepalive at any useful rate is GOAWAY'd for too_many_pings while
			// this server pings it every 54s. That asymmetry made the obvious
			// client-side mitigation actively harmful.
			grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
				MinTime:             conf.KeepaliveMinTime,
				PermitWithoutStream: true,
			}),
			grpc.MaxConcurrentStreams(maxConcurrentStreams),
		),
	}
	streamv1.RegisterBlockStreamServiceServer(g.grpcSrv, g)
	return g
}

// Run serves until Stop is called; grpc.ErrServerStopped on a clean stop is
// treated as normal by the caller.
func (g *GRPCServer) Run() error {
	lis, err := net.Listen("tcp", g.cfg.Addr)
	if err != nil {
		return err
	}
	g.log.Info("starting consumer stream grpc server", logger.WithString("addr", g.cfg.Addr))
	return g.grpcSrv.Serve(lis)
}

// Stop hard-stops the server, canceling active Subscribe streams so shutdown
// does not block on long-lived consumers.
func (g *GRPCServer) Stop() { g.grpcSrv.Stop() }

// Subscribe serves the consumer block stream.
//
// The client stream is only a token channel: the first message selects mode
// and topics, later ones carry a refreshed JWT. A client that sends one
// message and half-closes, which is what a server-streaming stub does, is
// served exactly as before, so the shape change is wire compatible.
func (g *GRPCServer) Subscribe(stream grpc.BidiStreamingServer[streamv1.SubscribeRequest, streamv1.BlockEvent]) error {
	req, err := stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			// Half-closed without selecting anything, so there is nothing to serve.
			return status.Error(codes.InvalidArgument, "no subscribe request received")
		}
		return err
	}
	mode, ok := normalizeMode(req.GetMode())
	if !ok {
		return status.Error(codes.InvalidArgument, "invalid mode")
	}
	if !topicsOK(req.GetTopics()...) {
		return status.Error(codes.InvalidArgument, "unsupported topics")
	}

	ctx := stream.Context()
	// The first token still comes from the header, so nothing changes for a
	// consumer that never refreshes.
	token := metadataToken(ctx)
	subject, err := g.auth.Authenticate(token)
	if err != nil {
		telemetry.RecordStreamAuthFailure()
		return status.Error(codes.Unauthenticated, "unauthorized")
	}
	connID, ok := g.limiter.acquire(subject)
	if !ok {
		return status.Error(codes.ResourceExhausted, "too many connections")
	}
	defer g.limiter.release(subject, connID)

	sub := g.hub.Subscribe(g.cfg.BufferSize)
	defer sub.Close()

	// Refreshed tokens arrive on their own goroutine because Recv blocks, but
	// they are applied on the send loop, which stays the only writer.
	refresh := make(chan string, 1)
	go recvTokens(stream, refresh)

	hb, hbC := heartbeatTicker(g.cfg.HeartbeatInterval)
	if hb != nil {
		defer hb.Stop()
	}
	ra, raC := reauthTicker(&g.cfg)
	if ra != nil {
		defer ra.Stop()
	}

	var live livenessState
	var reauthFailing bool
	raw := mode == modeRaw
	var lastDropped uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case t := <-refresh:
			// Verified on the send loop, which owns token, so a rejected
			// refresh cannot change who this connection is authenticated as.
			if _, err := g.auth.Authenticate(t); err != nil {
				telemetry.RecordStreamAuthFailure()
				continue
			}
			token = t
		case <-raC:
			// Re-verifying the token the connection last presented is what
			// makes expiry visible: the verifier checks exp, so no expiry
			// needs tracking here.
			if _, rerr := g.auth.Authenticate(token); rerr != nil {
				telemetry.RecordStreamReauthFailure()
				if g.cfg.ReauthMode == ReauthEnforce {
					g.log.Error("closing consumer stream, token no longer verifies (enforce mode)", rerr,
						logger.WithString("subject", subject))
					return status.Error(codes.Unauthenticated, "token expired, refresh required")
				}
				// Logged on the transition only: observe mode exists to learn
				// which consumers have not adopted refresh, and the metric
				// alone does not name them.
				if !reauthFailing {
					reauthFailing = true
					g.log.Error("consumer stream token no longer verifies, keeping stream (observe mode)", rerr,
						logger.WithString("subject", subject))
				}
			} else {
				reauthFailing = false
			}
		case <-hbC:
			last, expected, silence := live.snapshot(time.Now())
			f := &streamv1.BlockEvent{Frame: &streamv1.BlockEvent_Heartbeat{Heartbeat: &streamv1.Heartbeat{
				LastSlot: last, ExpectedSlot: expected, SilenceMs: silence,
			}}}
			if err := stream.Send(f); err != nil {
				return err
			}
			telemetry.RecordStreamHeartbeatSent()
		case ev, ok := <-sub.Events():
			if !ok {
				return nil
			}
			if d := sub.Dropped(); d != lastDropped {
				lastDropped = d
				lag := &streamv1.BlockEvent{Frame: &streamv1.BlockEvent_Lagged{Lagged: &streamv1.Lagged{Dropped: d}}}
				if err := stream.Send(lag); err != nil {
					return err
				}
			}
			if err := stream.Send(toProto(ev, raw)); err != nil {
				return err
			}
			live.observe(ev.Slot)
			telemetry.RecordStreamEventSent()
		}
	}
}

// recvTokens forwards refreshed tokens until the client stops sending. io.EOF
// is the normal end for a consumer that half-closes and never refreshes, so it
// ends this goroutine without disturbing the stream.
func recvTokens(stream grpc.BidiStreamingServer[streamv1.SubscribeRequest, streamv1.BlockEvent], out chan string) {
	for {
		m, err := stream.Recv()
		if err != nil {
			return
		}
		if t := m.GetToken(); t != "" {
			offerLatest(out, t)
		}
	}
}

func toProto(ev *streamhub.BlockEvent, raw bool) *streamv1.BlockEvent {
	b := &streamv1.Block{
		Slot:           ev.Slot,
		ProposerIndex:  ev.ProposerIndex,
		ParentRoot:     ev.ParentRoot,
		StateRoot:      ev.StateRoot,
		BlockSizeBytes: ev.BlockSizeBytes,
		Topic:          ev.Topic,
		Source:         string(ev.Source),
		ReceivedAtMs:   ev.ReceivedAtMs,
		GatewayId:      ev.GatewayID,
		ForkDigest:     ev.ForkDigest,
		Stale:          ev.Stale,
	}
	if raw {
		b.Raw = ev.Raw
	}
	return &streamv1.BlockEvent{Frame: &streamv1.BlockEvent_Block{Block: b}}
}

// metadataToken reads the consumer JWT from the "authorization" gRPC metadata,
// accepting either a bare token or a "Bearer <jwt>" value.
func metadataToken(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ""
	}
	tok := vals[0]
	if after, ok := strings.CutPrefix(tok, "Bearer "); ok {
		tok = after
	}
	return strings.TrimSpace(tok)
}
