package stream

import (
	"context"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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
	limiter *connLimiter
	grpcSrv *grpc.Server
}

// NewGRPCServer builds the consumer gRPC server. It does not start listening;
// call Run.
func NewGRPCServer(hub *streamhub.Service, auth ConsumerAuthenticator, cfg Config, log logger.AppLogger) *GRPCServer {
	cfg = withDefaults(cfg)
	g := &GRPCServer{
		hub:     hub,
		auth:    auth,
		cfg:     cfg,
		log:     log.With(logger.WithService("stream-grpc")),
		limiter: newConnLimiter(cfg.MaxConns, cfg.MaxConnsPerSub),
		grpcSrv: grpc.NewServer(),
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

// Subscribe authenticates and enforces caps before opening the stream, then
// drains the buffer as proto frames (metadata omits Raw); lagged on overflow.
func (g *GRPCServer) Subscribe(req *streamv1.SubscribeRequest, stream grpc.ServerStreamingServer[streamv1.BlockEvent]) error {
	mode, ok := normalizeMode(req.GetMode())
	if !ok {
		return status.Error(codes.InvalidArgument, "invalid mode")
	}
	if !topicsOK(req.GetTopics()...) {
		return status.Error(codes.InvalidArgument, "unsupported topics")
	}

	ctx := stream.Context()
	subject, err := g.auth.Authenticate(metadataToken(ctx))
	if err != nil {
		telemetry.RecordStreamAuthFailure()
		return status.Error(codes.Unauthenticated, "unauthorized")
	}
	if !g.limiter.acquire(subject) {
		return status.Error(codes.ResourceExhausted, "too many connections")
	}
	defer g.limiter.release(subject)

	sub := g.hub.Subscribe(g.cfg.BufferSize)
	defer sub.Close()

	raw := mode == modeRaw
	var lastDropped uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-sub.Events():
			if !ok {
				return nil
			}
			if d := sub.Dropped(); d != lastDropped {
				lastDropped = d
				if err := stream.Send(&streamv1.BlockEvent{Lagged: true, Dropped: d}); err != nil {
					return err
				}
			}
			if err := stream.Send(toProto(ev, raw)); err != nil {
				return err
			}
			telemetry.RecordStreamEventSent()
		}
	}
}

func toProto(ev *streamhub.BlockEvent, raw bool) *streamv1.BlockEvent {
	pe := &streamv1.BlockEvent{
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
		pe.Raw = ev.Raw
	}
	return pe
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
