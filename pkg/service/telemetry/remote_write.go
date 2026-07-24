package telemetry

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	commonconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promslog"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb/agent"
	"github.com/prometheus/prometheus/util/logging"
	promyaml "gopkg.in/yaml.v2"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

type mimirRemoteState struct {
	store *remote.Storage
	cfg   *config.AppConfig
	log   logger.AppLogger
}

var (
	mimirRemote    atomic.Pointer[mimirRemoteState]
	mimirRemoteReg atomic.Pointer[prometheus.Registry]
)

func mimirRemoteRegistry() *prometheus.Registry {
	return mimirRemoteReg.Load()
}

// refreshMimirRemoteWriteHeaders re-applies remote write config when the bearer token rotates.
func refreshMimirRemoteWriteHeaders() {
	st := mimirRemote.Load()
	if st == nil {
		return
	}
	promCfg, err := buildMimirPromConfig(st.cfg, mimirScrapeInterval)
	if err != nil {
		st.log.Error("rebuild remote-write config after token refresh", err)
		return
	}
	if err = st.store.ApplyConfig(promCfg); err != nil {
		st.log.Error("reapply remote-write config after token refresh", err)
	}
}

const (
	mimirRemoteWritePath     = "/api/v1/push"
	mimirRemoteFlushDeadline = time.Minute
	mimirScrapeInterval      = 15 * time.Second
	mimirScrapeTimeout       = 10 * time.Second
	metricsProbeTimeout      = 5 * time.Second
)

// readyScrapeManager implements remote.ReadyScrapeManager for WriteStorage startup ordering.
type readyScrapeManager struct {
	m atomic.Pointer[scrape.Manager]
}

func (rm *readyScrapeManager) Set(m *scrape.Manager) {
	rm.m.Store(m)
}

func (rm *readyScrapeManager) Get() (*scrape.Manager, error) {
	if m := rm.m.Load(); m != nil {
		return m, nil
	}
	return nil, fmt.Errorf("scrape manager not ready")
}

// startMimirRemoteWrite runs prometheus agent-style loopback scrape -> WAL -> remote write to mimir.
func startMimirRemoteWrite(ctx context.Context, log logger.AppLogger, cfg *config.AppConfig) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := runMimirRemoteWrite(ctx, log, cfg); err != nil && ctx.Err() == nil {
			log.Error("mimir remote write stopped", err)
		}
	}()
	return done
}

func runMimirRemoteWrite(ctx context.Context, log logger.AppLogger, cfg *config.AppConfig) error {
	if strings.TrimSpace(cfg.RemotePushWALDir) == "" {
		return fmt.Errorf("remote_push_wal_dir is required")
	}
	walDir := filepath.Clean(cfg.RemotePushWALDir)
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		return fmt.Errorf("create remote push wal dir %q: %w", walDir, err)
	}

	promCfg, err := buildMimirPromConfig(cfg, mimirScrapeInterval)
	if err != nil {
		return err
	}

	slogLogger := promslog.New(&promslog.Config{}).With("component", "mimir_remote_write")

	readyScrape := &readyScrapeManager{}

	remoteReg := prometheus.NewRegistry()
	mimirRemoteReg.Store(remoteReg)
	defer mimirRemoteReg.Store(nil)

	remoteStore := remote.NewStorage(
		slogLogger,
		remoteReg,
		func() (int64, error) { return 0, nil },
		walDir,
		mimirRemoteFlushDeadline,
		readyScrape,
		false,
	)
	mimirRemote.Store(&mimirRemoteState{store: remoteStore, cfg: cfg, log: log})
	defer mimirRemote.Store(nil)
	defer func() {
		if closeErr := remoteStore.Close(); closeErr != nil {
			log.Error("mimir remote write close", closeErr)
		}
	}()

	agentOpts := agent.DefaultOptions()
	agentDB, err := agent.Open(slogLogger, remoteReg, remoteStore, walDir, agentOpts)
	if err != nil {
		return fmt.Errorf("open agent wal at %q: %w", walDir, err)
	}
	defer func() {
		if closeErr := agentDB.Close(); closeErr != nil {
			log.Error("mimir agent wal close", closeErr)
		}
	}()
	agentDB.SetWriteNotified(remoteStore)

	if err = remoteStore.ApplyConfig(promCfg); err != nil {
		return fmt.Errorf("apply remote write config: %w", err)
	}

	sdMetrics, err := discovery.CreateAndRegisterSDMetrics(remoteReg)
	if err != nil {
		return fmt.Errorf("register discovery metrics: %w", err)
	}

	scrapeCtx, scrapeCancel := context.WithCancel(ctx)
	defer scrapeCancel()

	discoveryMgr := discovery.NewManager(
		scrapeCtx,
		slogLogger.With("component", "discovery"),
		remoteReg,
		sdMetrics,
		discovery.Name("scrape"),
	)

	scrapeMgr, err := scrape.NewManager(
		&scrape.Options{},
		slogLogger.With("component", "scrape"),
		logging.NewJSONFileLogger,
		agentDB,
		nil,
		remoteReg,
	)
	if err != nil {
		return fmt.Errorf("create scrape manager: %w", err)
	}
	readyScrape.Set(scrapeMgr)

	if err = scrapeMgr.ApplyConfig(promCfg); err != nil {
		return fmt.Errorf("apply scrape config: %w", err)
	}

	scfgs, err := promCfg.GetScrapeConfigs()
	if err != nil {
		return err
	}
	sdConfigs := make(map[string]discovery.Configs, len(scfgs))
	for _, sc := range scfgs {
		sdConfigs[sc.JobName] = sc.ServiceDiscoveryConfigs
	}
	if err = discoveryMgr.ApplyConfig(sdConfigs); err != nil {
		return fmt.Errorf("apply discovery config: %w", err)
	}

	if err := waitForMetricsEndpoint(ctx, cfg.TelemetryPort); err != nil {
		return err
	}

	log.Info("starting mimir remote write",
		logger.WithString("url", mimirRemoteWriteURL(cfg)),
		logger.WithString("wal_dir", walDir),
		logger.WithInt("telemetry_port", cfg.TelemetryPort),
		logger.WithBool("bearer_token_set", currentPushToken() != ""),
	)

	go func() { _ = discoveryMgr.Run() }()
	go func() { _ = scrapeMgr.Run(discoveryMgr.SyncCh()) }()
	<-ctx.Done()
	scrapeMgr.Stop()
	return ctx.Err()
}

func waitForMetricsEndpoint(ctx context.Context, port int) error {
	if port <= 0 {
		return fmt.Errorf("telemetry port is not configured")
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	metricsURL := fmt.Sprintf("http://127.0.0.1:%d/metrics", port)
	client := &http.Client{Timeout: metricsProbeTimeout}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(deadlineCtx, http.MethodGet, metricsURL, http.NoBody)
		if err != nil {
			return fmt.Errorf("create metrics probe request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-deadlineCtx.Done():
			return fmt.Errorf("metrics endpoint %s not ready: %w", metricsURL, deadlineCtx.Err())
		case <-ticker.C:
		}
	}
}

func mimirRemoteWriteURL(cfg *config.AppConfig) string {
	return strings.TrimSuffix(cfg.RemotePushMimirURL, "/") + mimirRemoteWritePath
}

func buildMimirPromConfig(cfg *config.AppConfig, scrapeInterval time.Duration) (*promconfig.Config, error) {
	pushURL, err := url.Parse(mimirRemoteWriteURL(cfg))
	if err != nil {
		return nil, fmt.Errorf("parse mimir remote write url: %w", err)
	}

	rwCfg := promconfig.DefaultRemoteWriteConfig
	rwCfg.URL = &commonconfig.URL{URL: pushURL}
	rwCfg.Name = "mimir"
	if token := currentPushToken(); token != "" {
		rwCfg.HTTPClientConfig.Authorization = &commonconfig.Authorization{
			Type:        "Bearer",
			Credentials: commonconfig.Secret(token),
		}
	}

	target := fmt.Sprintf("127.0.0.1:%d", cfg.TelemetryPort)
	scrapeCfg := &promconfig.ScrapeConfig{
		JobName:        "optimum_gateway",
		ScrapeInterval: model.Duration(scrapeInterval),
		ScrapeTimeout:  model.Duration(mimirScrapeTimeout),
		MetricsPath:    "/metrics",
		Scheme:         "http",
		ServiceDiscoveryConfigs: discovery.Configs{
			discovery.StaticConfig{
				&targetgroup.Group{
					Targets: []model.LabelSet{
						{model.AddressLabel: model.LabelValue(target)},
					},
				},
			},
		},
	}

	raw := &promconfig.Config{
		GlobalConfig: promconfig.GlobalConfig{
			ScrapeInterval:     model.Duration(scrapeInterval),
			EvaluationInterval: model.Duration(scrapeInterval),
		},
		ScrapeConfigs:      []*promconfig.ScrapeConfig{scrapeCfg},
		RemoteWriteConfigs: []*promconfig.RemoteWriteConfig{&rwCfg},
	}
	prevMarshalSecrets := commonconfig.MarshalSecretValue
	commonconfig.MarshalSecretValue = true
	yml, err := promyaml.Marshal(raw)
	commonconfig.MarshalSecretValue = prevMarshalSecrets
	if err != nil {
		return nil, fmt.Errorf("marshal prometheus config: %w", err)
	}
	loaded, err := promconfig.Load(string(yml), promslog.NewNopLogger())
	if err != nil {
		return nil, fmt.Errorf("load prometheus config: %w", err)
	}
	return loaded, nil
}
