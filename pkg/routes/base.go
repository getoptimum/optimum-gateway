package routes

import (
	"context"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // pprof binds to cfg.PProfAddr (default localhost; use OPT_PPROF_ADDR to tune)
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	gateway "github.com/getoptimum/optimum-gateway/pkg/service/gossipsub-gateway"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

type Server struct {
	appAddr    string
	log        logger.AppLogger
	srvGateway *gateway.Service
	httpEngine *fiber.App
	cfg        *config.AppConfig
}

func NewAppRouter(
	log logger.AppLogger,
	srvGateway *gateway.Service,
	cfg *config.AppConfig,
	address string,
) *Server {
	app := &Server{
		appAddr: address,
		httpEngine: fiber.New(fiber.Config{
			GETOnly:      true,
			AppName:      "optimum-gateway",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  120 * time.Second,
		}),
		srvGateway: srvGateway,
		cfg:        cfg,
		log:        log.With(logger.WithService("http")),
	}
	app.httpEngine.Use(recover.New())
	if app.cfg.TelemetryEnable {
		app.httpEngine.Get("/metrics", adaptor.HTTPHandler(promhttp.HandlerFor(telemetry.HTTPMetricsGatherer, promhttp.HandlerOpts{})))
	}
	app.initRoutes()
	return app
}

func (s *Server) initRoutes() {
	s.httpEngine.Get("/", s.handleRoot)
	s.httpEngine.Get("/health", s.handleHealth)
	s.httpEngine.Get("/api/v1/self_info", s.handleSelfInfo)
}

// handleRoot returns 200 when the process and HTTP server are responding.
func (s *Server) handleRoot(ctx fiber.Ctx) error {
	return ctx.JSON(fiber.Map{"status": "ok"})
}

func (s *Server) handleHealth(ctx fiber.Ctx) error {
	resp, code := s.srvGateway.BuildHealthResponse()
	if lastMs := resp.Checks["last_block_age_sec"]; lastMs.Value != nil {
		telemetry.SetLastBlockReceivedTimestamp(float64(time.Now().Unix() - int64(*lastMs.Value)))
	}
	return ctx.Status(code).JSON(resp)
}

func (s *Server) handleSelfInfo(ctx fiber.Ctx) error {
	info := s.srvGateway.GetHostInfo()
	totalLibP2PPeers, libP2PPeersPerTopic, libP2PPeerIDs, libP2PPeerIDsPerTopic := s.srvGateway.GetLibP2PPeers()
	totalMumP2PPeers, mumP2PPeersPerTopic, mumP2PPeerIDs, mumP2PPeerIDsPerTopic := s.srvGateway.GetMumP2PPeers()
	rlncCfg := s.cfg.GetDCRotator().Get()
	pairedWith := ""
	if c := s.srvGateway.GetAuthManager().OwnClaims(); c != nil {
		pairedWith = c.Type.String()
	}
	return ctx.JSON(map[string]any{
		"peer_id":                 info.ID.String(),
		"cl_health":               telemetry.HealthCL(),
		"mump2p_health":           telemetry.HealthMUM(),
		"version":                 s.cfg.Version,
		"commit_hash":             s.cfg.CommitHash,
		"gateway_id":              s.cfg.GatewayID,
		"gateway_cluster_id":      s.cfg.GatewayClusterID,
		"paired_with":             pairedWith,
		"remote_url":              s.cfg.RemoteBootstrapURL,
		"propagation_enabled":     s.cfg.PropagationEnabled(),
		"fork_digest":             s.srvGateway.GetForkDigestManager().ActiveDigest(),
		"skip_messages_from_self": s.cfg.GetSkipMessagesFromSelf(),
		"rlnc_config": map[string]any{
			"random_message_size_bytes":  rlncCfg.RandomMessageSize,
			"rlnc_shard_factor":          rlncCfg.ShardFactor,
			"publisher_shard_multiplier": rlncCfg.PublisherShardMultiplier,
			"forward_shard_threshold":    rlncCfg.ForwardShardThreshold,
			"max_shard_size":             s.maxShardSize(),
		},
		"chain": s.srvGateway.GetForkDigestManager().AppChain().String(),
		"libp2p": map[string]any{
			"total_peers":        totalLibP2PPeers,
			"peers_per_topic":    libP2PPeersPerTopic,
			"peer_ids":           libP2PPeerIDs,
			"peer_ids_per_topic": libP2PPeerIDsPerTopic,
			"multiaddrs":         info.Addrs,
			"direct_peers":       s.srvGateway.GetDirectLibP2PPeers(),
		},
		"mump2p": map[string]any{
			"total_peers":        totalMumP2PPeers,
			"peers_per_topic":    mumP2PPeersPerTopic,
			"peer_ids":           mumP2PPeerIDs,
			"peer_ids_per_topic": mumP2PPeerIDsPerTopic,
		},
		"datagram": s.datagramInfo(),
	})
}

// maxShardSize reports the RLNC shard size cap the running node coded at, and 0
// before the mump2p node exists. Unlike the other rlnc_config entries it is not
// served by the dynamic config, so it has to come from the node itself.
func (s *Server) maxShardSize() uint32 {
	engine := s.srvGateway.GetMumP2PEngine()
	if engine == nil {
		return 0
	}
	return engine.EffectiveMaxShardSize()
}

// datagramInfo reports whether the datagram data plane actually carries traffic.
// A send with no confirmed path takes the stream fallback silently, so a run that
// never validated one is otherwise indistinguishable from a working one.
func (s *Server) datagramInfo() map[string]any {
	info := map[string]any{
		"enabled":         s.cfg.DatagramEnable,
		"local_addr":      "",
		"paths_confirmed": 0,
		"peers_total":     0,
		"peers":           map[string]any{},
	}

	engine := s.srvGateway.GetMumP2PEngine()
	if !s.cfg.DatagramEnable || engine == nil {
		return info
	}
	if addr, ok := engine.DatagramLocalAddr(); ok {
		info["local_addr"] = addr.String()
	}

	peers := engine.GetPeers()
	perPeer := make(map[string]any, len(peers))
	confirmed := 0
	for _, p := range peers {
		pathOK := engine.DatagramPathConfirmed(p)
		if pathOK {
			confirmed++
		}
		expiresAt := ""
		if at, ok := engine.DatagramSessionExpiry(p); ok {
			expiresAt = at.UTC().Format(time.RFC3339)
		}
		perPeer[p.String()] = map[string]any{
			"path_confirmed":     pathOK,
			"session_expires_at": expiresAt,
		}
	}

	info["paths_confirmed"] = confirmed
	info["peers_total"] = len(peers)
	info["peers"] = perPeer
	return info
}

// Run starts the HTTP Server. ctx is used to bound the lifetime of the
// pprof memstat dumper; the Fiber listener itself is stopped via Stop().
func (s *Server) Run(ctx context.Context) error {
	s.log.Info("starting http server", logger.WithString("port", s.appAddr))
	if s.cfg.EnablePProf {
		go func() {
			go utils.DumpMemStat(ctx, s.log, 5*time.Second)
			s.log.Info("starting pprof server", logger.WithString("addr", s.cfg.PProfAddr))
			if errP := http.ListenAndServe(s.cfg.PProfAddr, nil); errP != nil { //nolint:gosec // bind address is configurable; default is loopback
				s.log.Error("failed to start pprof server", errP)
			}
		}()
	}
	return s.httpEngine.Listen(s.appAddr, fiber.ListenConfig{
		DisableStartupMessage: true,
	})
}

func (s *Server) Stop() error {
	return s.httpEngine.Shutdown()
}
