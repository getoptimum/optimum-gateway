package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonio "github.com/getoptimum/optimum-common/pkg/io"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/routes"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	gateway "github.com/getoptimum/optimum-gateway/pkg/service/gossipsub-gateway"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
	"github.com/getoptimum/optimum-gateway/pkg/service/stream"
	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// injected at build time using -ldflags
var (
	confFile = flag.String("config", "config/app_conf.yml", "Path to the configuration file (YAML format). If not provided, environment variables will be used.")
)

func main() {
	flag.Parse()
	ctx, cancel := context.WithCancel(context.Background())
	appConf, err := config.LoadConfig(*confFile)
	if err != nil {
		logger.NewAppSLogger(logger.Production).Fatal("unable to load config", err)
	}
	appLogger := logger.NewAppSLogger(logger.LogMode(appConf.LogLevel),
		logger.WithGatewayID(appConf.GatewayID),
		logger.WithClusterID(appConf.GatewayClusterID),
	)

	// Create rotating file writer for debug logs
	rotatingFile, err := commonio.NewRotatingFileWriter("/gateway/logs/gateway-debug.log", commonio.DefaultMaxFileSize)
	if err != nil {
		// Fall back to stdout-only logging if file creation fails
		appLogger.Error("failed to create rotating log file", err)
		rotatingFile = nil
	}

	// Initialize logger with stdout and rotating file
	var writers []io.Writer
	writers = append(writers, os.Stdout)
	if rotatingFile != nil {
		writers = append(writers, rotatingFile)
		defer rotatingFile.Close()
	}

	// Initialize auth manager.
	authMgr, err := auth_token.New(ctx, appLogger, appConf)
	if err != nil {
		appLogger.Fatal("unable to initialize auth_token manager", err)
	}
	if _, mintErr := authMgr.Token(ctx); mintErr != nil {
		appLogger.Fatal("initial JWT mint failed", mintErr)
	}
	authMgr.Start(ctx)

	// Resolve the JWT-sourced identity. When auth is enabled, the JWT's
	// `chain_id` and `sub` claims are authoritative. In dev mode they're
	// empty and InitRuntime keeps whatever the yaml/env/default supplied
	// (OPT_DEV_CHAIN / OPT_GATEWAY_ID).
	var chainID, gatewayID, gatewayType, orgID string
	if c := authMgr.OwnClaims(); c != nil {
		chainID = c.ChainID
		gatewayID = c.Subject
		gatewayType = c.Type.String()
		orgID = authMgr.OperatorID()
	}
	if err = appConf.InitRuntime(ctx, appLogger, chainID, gatewayID, gatewayType, orgID); err != nil {
		appLogger.Fatal("unable to initialize runtime", err)
	}
	if authMgr.IsEnabled() {
		appConf.MetaLabels = authMgr.GatewayLabels()
	}

	// remote_push_enable currently requires telemetry_enable=true because Mimir
	// push depends on the Prometheus registry initialized by InitMetrics.
	var lokiWriter *telemetry.LokiWriter
	if appConf.RemotePushEnable {
		if authMgr.IsEnabled() {
			// operator_id comes from the /auth/token response body, not the JWT.
			lokiWriter = telemetry.NewLokiWriter(appConf, authMgr.OperatorID(), appLogger)
			writers = append(writers, lokiWriter)
		} else {
			appLogger.Info("remote_push_enable is true but auth is disabled — loki push disabled")
		}
	}

	l := logger.InitLogger(writers,
		logger.LogMode(appConf.LogLevel),
		logger.WithGatewayID(appConf.GatewayID),
		logger.WithClusterID(appConf.GatewayClusterID),
	)
	l.Info("app starting",
		logger.WithString("version", appConf.Version),
		logger.WithString("commit", appConf.CommitHash),
		logger.WithGatewayID(appConf.GatewayID),
		logger.WithClusterID(appConf.GatewayClusterID),
		logger.WithInt("libp2p_port", appConf.AgentLibP2PPort),
		logger.WithInt("mump2p_port", appConf.AgentMumP2PPort),
		logger.WithInt("telemetry_port", appConf.TelemetryPort),
		logger.WithBool("telemetry_enable", appConf.TelemetryEnable),
	)

	if appConf.RemotePushEnable && !appConf.TelemetryEnable {
		l.Fatal("invalid config: remote_push_enable requires telemetry_enable=true", nil)
	}

	var mimirDone <-chan struct{}
	if appConf.TelemetryEnable {
		l.Info("initializing telemetry")
		if appConf.RemotePushEnable && !authMgr.IsEnabled() {
			l.Info("remote_push_enable is true but auth is disabled — remote push disabled")
		}
		mimirDone = telemetry.InitMetrics(ctx, l, appConf)
		authMgr.RefreshAuthMetrics()
	} else {
		l.Info("telemetry is disabled")
	}

	var lokiDone <-chan struct{}
	if lokiWriter != nil {
		lokiDone = lokiWriter.Start(ctx)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	srvMessageRouter, err := message_router.NewService(ctx, appConf, l, authMgr)
	if err != nil {
		l.Fatal("unable to initialize message router", err)
	}

	// OPT_DEV_VALIDATOR_INDEXES seeds the router's known-validator set at
	// boot. Only honored when auth is off — with auth on, the JWT mint
	// response is authoritative and the env-seeded set would be overwritten
	// on the first sync tick anyway.
	if !authMgr.IsEnabled() {
		if raw := os.Getenv("OPT_DEV_VALIDATOR_INDEXES"); raw != "" {
			indexes, parseErr := utils.ParseCommaSeparatedUint64s(raw)
			if parseErr != nil {
				l.Fatal("invalid OPT_DEV_VALIDATOR_INDEXES (want comma-separated uint64s)", parseErr)
			}
			srvMessageRouter.SetKnownValidators(indexes)
			l.Info("seeded known validators from OPT_DEV_VALIDATOR_INDEXES",
				logger.WithInt("count", len(indexes)))
		}
	}

	// Consumer block-stream (ADR-0011), opt-in and off by default. The hub must
	// exist before the gateway so it can be wired as an emit sink.
	var streamServer *stream.Server
	var hub *streamhub.Service
	if appConf.StreamEnable {
		hub = streamhub.New()
		authenticator := stream.NewConsumerAuthenticator(authMgr, appConf.StreamRequireAuth)
		streamServer = stream.NewServer(hub, authenticator, stream.Config{
			Addr:           appConf.StreamAddr,
			MaxConns:       appConf.StreamMaxConns,
			MaxConnsPerSub: appConf.StreamMaxConnsPerSub,
			BufferSize:     appConf.StreamBufferSize,
		}, l)
	}

	srvGateway, err := gateway.NewService(ctx, l, appConf, srvMessageRouter, authMgr, gateway.WithStreamHub(hub))
	if err != nil {
		l.Fatal("unable to initialize gossipsub gateway", err)
	}
	if err = srvGateway.Run(); err != nil {
		l.Fatal("failed to run service", err)
	}

	// HTTP server, but expose /metrics only if telemetry is enabled.
	appRouter := routes.NewAppRouter(l, srvGateway, appConf, fmt.Sprintf(":%d", appConf.TelemetryPort))
	go func() {
		if runErr := appRouter.Run(ctx); runErr != nil {
			l.Fatal("failed to run http server", runErr)
		}
	}()

	if streamServer != nil {
		go func() {
			if runErr := streamServer.Run(); runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
				l.Fatal("failed to run consumer stream server", runErr)
			}
		}()
	}

	<-c // This blocks the main thread until an interrupt is received
	cancel()
	_ = appRouter.Stop()
	if streamServer != nil {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		_ = streamServer.Stop(shutdownCtx)
		cancelShutdown()
	}
	srvGateway.Stop()
	if lokiDone != nil {
		<-lokiDone // wait for final Loki flush to complete
	}
	if mimirDone != nil {
		<-mimirDone // wait for Mimir remote-write shutdown to complete
	}
}
