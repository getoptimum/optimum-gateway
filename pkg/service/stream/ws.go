package stream

import (
	"context"
	"encoding/json"
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
	wsSubprotocol      = "optimum.stream.v1"
	bearerSubproto     = "bearer."
	defaultTopic       = "beacon_block"
	modeMetadata       = "metadata"
	modeRaw            = "raw"
	frameTypeBlock     = "block"
	frameTypeLagged    = "lagged"
	frameTypeHeartbeat = "heartbeat"

	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	// Matches nginx's 8k header limit, which already bounds this same token in
	// Authorization at connect, so anything that can connect can refresh.
	maxReadBytes = 8 << 10
)

// Config carries the transport's static limits, sourced from AppConfig by the
// caller so this package stays independent of pkg/config.
type Config struct {
	Addr           string
	MaxConns       int
	MaxConnsPerSub int
	BufferSize     int
	// HeartbeatInterval paces the liveness frame each transport emits from its
	// own send loop. Zero disables it.
	HeartbeatInterval time.Duration
	// ReauthMode is off, observe or enforce. Re-auth re-verifies the token the
	// connection last presented, so an expired one fails on its own.
	ReauthMode string
	// ReauthInterval paces that re-verification.
	ReauthInterval time.Duration
	// KeepaliveMinTime is the shortest client ping interval the gRPC server
	// tolerates; the library default of 5m rejects any useful rate.
	KeepaliveMinTime time.Duration
	// Limiter is shared across transports so caps stay global; withDefaults
	// creates one if nil.
	Limiter *ConnLimiter
}

// Server exposes streamhub over WebSocket on its own listener, keeping the
// consumer surface off the gateway's /metrics and /health port (ADR-0011).
type Server struct {
	hub      *streamhub.Service
	auth     ConsumerAuthenticator
	cfg      Config
	log      logger.AppLogger
	limiter  *ConnLimiter
	upgrader websocket.Upgrader
	httpSrv  *http.Server
}

// NewServer builds the consumer WebSocket server. It does not start listening;
// call Run.
func NewServer(hub *streamhub.Service, auth ConsumerAuthenticator, cfg *Config, log logger.AppLogger) *Server {
	conf := withDefaults(cfg)
	s := &Server{
		hub:     hub,
		auth:    auth,
		cfg:     conf,
		log:     log.With(logger.WithService("stream-ws")),
		limiter: conf.Limiter,
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
	token := bearerToken(r)
	subject, err := s.auth.Authenticate(token)
	if err != nil {
		telemetry.RecordStreamAuthFailure()
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Enforce caps before the upgrade too, so a rejected connection allocates
	// no subscriber.
	release, ok := s.limiter.acquire(subject)
	if !ok {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote the error response.
		release()
		return
	}

	sub := s.hub.Subscribe(s.cfg.BufferSize)
	go s.serve(conn, sub, subject, token, release, mode == modeRaw)
}

// subject and token are still needed here, unlike the plain heartbeat case:
// re-auth re-verifies the token and names the subject when it cuts a stream.
func (s *Server) serve(conn *websocket.Conn, sub *streamhub.Subscription, subject, token string, release func(), raw bool) {
	// sub.Close() deletes the per-connection drop counter, so it can't leak;
	// release() frees the cap slot. Both run on every exit path.
	defer func() {
		_ = conn.Close()
		sub.Close()
		release()
	}()

	conn.SetReadLimit(maxReadBytes)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	// Read pump: refreshes the idle deadline, forwards a refreshed token, and
	// closes done on a read error. Read-only in every other sense.
	done := make(chan struct{})
	refresh := make(chan string, 1)
	go func() {
		defer close(done)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var m struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(msg, &m) != nil || m.Token == "" {
				continue
			}
			offerLatest(refresh, m.Token)
		}
	}()

	ping := time.NewTicker(pingPeriod)
	defer ping.Stop()
	hb, hbC := optionalTicker(s.cfg.HeartbeatInterval)
	defer stopTicker(hb)

	ra, raC := optionalTicker(s.cfg.reauthInterval())
	defer stopTicker(ra)

	auth := connAuth{cfg: &s.cfg, auth: s.auth, log: s.log, subject: subject}
	var live livenessState
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
			live.observe(ev.Slot)
			telemetry.RecordStreamEventSent()
		case t := <-refresh:
			if auth.accept(t) {
				token = t
			}
		case <-raC:
			if auth.reverify(token) {
				// Say why: a weeks-long consumer must tell an auth cut from a
				// dead network. The only close frame we send.
				_ = conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "token expired, refresh required"),
					time.Now().Add(writeWait))
				return
			}
		case <-hbC:
			last, expected, silence := live.snapshot(time.Now())
			if err := s.writeJSON(conn, heartbeatFrame{
				Type: frameTypeHeartbeat, LastSlot: last, ExpectedSlot: expected, SilenceMs: silence,
			}); err != nil {
				return
			}
			telemetry.RecordStreamHeartbeatSent()
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

// heartbeatFrame proves the feed is alive during a quiet stretch. A data frame,
// not a control ping: browsers cannot observe WebSocket ping/pong at all.
type heartbeatFrame struct {
	Type         string `json:"type"`
	LastSlot     uint64 `json:"last_slot"`
	ExpectedSlot uint64 `json:"expected_slot"`
	SilenceMs    uint64 `json:"silence_ms"`
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
