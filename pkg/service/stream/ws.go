package stream

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
)

const (
	// streamPath is the single read-only consumer endpoint (ADR-0011).
	streamPath = "/api/v1/stream/blocks"
	// wsSubprotocol is the marker the server negotiates; the bearer token is
	// carried alongside it but never echoed back as the selected subprotocol.
	wsSubprotocol   = "optimum.stream.v1"
	bearerSubproto  = "bearer."
	defaultTopic    = "beacon_block"
	modeMetadata    = "metadata"
	modeRaw         = "raw"
	frameTypeBlock  = "block"
	frameTypeLagged = "lagged"

	writeWait    = 10 * time.Second
	pongWait     = 60 * time.Second
	pingPeriod   = (pongWait * 9) / 10
	maxReadBytes = 1 << 10 // consumers are read-only; cap inbound frames.
)

// Config carries the transport's static limits, sourced from AppConfig by the
// caller so this package stays independent of pkg/config.
type Config struct {
	Addr           string
	MaxConns       int
	MaxConnsPerSub int
	BufferSize     int
}

// Server exposes streamhub over WebSocket on its own listener, keeping the
// consumer surface off the gateway's /metrics and /health port (ADR-0011).
type Server struct {
	hub      *streamhub.Service
	auth     ConsumerAuthenticator
	cfg      Config
	log      logger.AppLogger
	limiter  *connLimiter
	upgrader websocket.Upgrader
	httpSrv  *http.Server
}

// NewServer builds the consumer WebSocket server. It does not start listening;
// call Run.
func NewServer(hub *streamhub.Service, auth ConsumerAuthenticator, cfg Config, log logger.AppLogger) *Server {
	cfg = withDefaults(cfg)
	s := &Server{
		hub:     hub,
		auth:    auth,
		cfg:     cfg,
		log:     log.With(logger.WithService("stream-ws")),
		limiter: newConnLimiter(cfg.MaxConns, cfg.MaxConnsPerSub),
		upgrader: websocket.Upgrader{
			// JWT gates access (not Origin; TLS/proxy is the exposure control).
			// Offering only the marker means gorilla never selects bearer.<jwt>.
			Subprotocols: []string{wsSubprotocol},
			CheckOrigin:  func(*http.Request) bool { return true },
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc(streamPath, s.handle)
	s.httpSrv = &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: writeWait,
	}
	return s
}

// Run serves until Stop is called; it returns http.ErrServerClosed on a clean
// shutdown, which the caller treats as normal.
func (s *Server) Run() error {
	s.log.Info("starting consumer stream server", logger.WithString("addr", s.cfg.Addr))
	return s.httpSrv.ListenAndServe()
}

// Stop drains in-flight requests within ctx.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpSrv.Shutdown(ctx)
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	mode, ok := normalizeMode(r.URL.Query().Get("mode"))
	if !ok {
		http.Error(w, "invalid mode", http.StatusBadRequest)
		return
	}
	if !topicsOK(r.URL.Query().Get("topics")) {
		http.Error(w, "unsupported topics", http.StatusBadRequest)
		return
	}

	// Authenticate before the upgrade: a rejected consumer is never subscribed.
	subject, err := s.auth.Authenticate(bearerToken(r))
	if err != nil {
		telemetry.RecordStreamAuthFailure()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Enforce caps before the upgrade too, so a rejected connection allocates
	// no subscriber.
	if !s.limiter.acquire(subject) {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote the error response.
		s.limiter.release(subject)
		return
	}

	sub := s.hub.Subscribe(s.cfg.BufferSize)
	go s.serve(conn, sub, subject, mode == modeRaw)
}

func (s *Server) serve(conn *websocket.Conn, sub *streamhub.Subscription, subject string, raw bool) {
	// sub.Close() deletes the per-connection drop counter, so it can't leak;
	// release() frees the cap slot. Both run on every exit path.
	defer func() {
		_ = conn.Close()
		sub.Close()
		s.limiter.release(subject)
	}()

	conn.SetReadLimit(maxReadBytes)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Read pump: consumers are read-only, so this only refreshes the idle
	// deadline and closes done on any read error to unblock the writer.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	ping := time.NewTicker(pingPeriod)
	defer ping.Stop()

	var lastDropped uint64
	for {
		select {
		case <-done:
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			// Surface overflow first so the consumer learns it lagged before the
			// next event lands.
			if d := sub.Dropped(); d != lastDropped {
				lastDropped = d
				if err := s.writeJSON(conn, laggedFrame{Type: frameTypeLagged, Dropped: d}); err != nil {
					return
				}
			}
			if err := s.writeBlock(conn, ev, raw); err != nil {
				return
			}
			telemetry.RecordStreamEventSent()
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// blockFrame inlines BlockEvent's JSON fields next to the frame type tag.
type blockFrame struct {
	Type string `json:"type"`
	*streamhub.BlockEvent
}

// laggedFrame reports the connection's cumulative dropped count after overflow.
type laggedFrame struct {
	Type    string `json:"type"`
	Dropped uint64 `json:"dropped"`
}

func (s *Server) writeBlock(conn *websocket.Conn, ev *streamhub.BlockEvent, raw bool) error {
	f := blockFrame{Type: frameTypeBlock, BlockEvent: ev}
	if !raw && ev.Raw != nil {
		// ev is shared read-only; copy before dropping Raw for metadata mode.
		cp := *ev
		cp.Raw = nil
		f.BlockEvent = &cp
	}
	return s.writeJSON(conn, f)
}

func (s *Server) writeJSON(conn *websocket.Conn, v any) error {
	_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
	return conn.WriteJSON(v)
}

// bearerToken reads the consumer JWT from the Authorization header, or from the
// "bearer.<jwt>" Sec-WebSocket-Protocol offer that browsers must use instead.
func bearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if tok, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(tok)
		}
	}
	for _, p := range websocket.Subprotocols(r) {
		if tok, ok := strings.CutPrefix(p, bearerSubproto); ok {
			return tok
		}
	}
	return ""
}
