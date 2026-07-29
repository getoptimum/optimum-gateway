package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

const (
	testGaugeValueReplay  = 42
	testGaugeValueRestart = 1

	testMetricWalReplay  = "wal_replay_test_gauge"
	testMetricWalRestart = "wal_restart_test_gauge"

	walEventuallyTimeout  = 30 * time.Second
	walEventuallyInterval = 500 * time.Millisecond

	pushEventuallyTimeout  = 45 * time.Second
	pushEventuallyInterval = time.Second

	doneWaitSlack = 5 * time.Second
)

func waitMimirDone(t *testing.T, done <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(mimirRemoteFlushDeadline + doneWaitSlack):
		t.Fatal(msg)
	}
}

func testTelemetryPort(t *testing.T, metricsBaseURL string) int {
	t.Helper()
	u, err := url.Parse(metricsBaseURL)
	require.NoError(t, err)
	port, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return port
}

func walHasData(t *testing.T, walDir string) bool {
	t.Helper()
	var found bool
	err := filepath.WalkDir(walDir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		found = true
		return filepath.SkipAll
	})
	require.NoError(t, err)
	return found
}

func TestBuildMimirPromConfig(t *testing.T) {
	wantToken := "test-token"
	SetPushToken(wantToken)
	t.Cleanup(func() { SetPushToken("") })

	cfg := &config.AppConfig{
		RemotePushMimirURL: "https://v2-mimir.example.test",
		TelemetryPort:      48123,
	}

	t.Run("bearer_token", func(t *testing.T) {
		promCfg, err := buildMimirPromConfig(cfg, mimirScrapeInterval)
		require.NoError(t, err)
		require.Len(t, promCfg.RemoteWriteConfigs, 1)
		auth := promCfg.RemoteWriteConfigs[0].HTTPClientConfig.Authorization
		require.NotNil(t, auth)
		require.Equal(t, "Bearer", auth.Type)
		require.Equal(t, wantToken, string(auth.Credentials))
	})

	t.Run("concurrent", func(t *testing.T) {
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range 10 {
					_, err := buildMimirPromConfig(cfg, mimirScrapeInterval)
					require.NoError(t, err)
				}
			}()
		}
		wg.Wait()
	})
}

func TestMimirRemoteWrite_shutdown(t *testing.T) {
	setTestToken(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == mimirRemoteWritePath {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	metricsSrv := httptest.NewServer(promhttp.HandlerFor(
		prometheus.NewRegistry(),
		promhttp.HandlerOpts{},
	))
	t.Cleanup(metricsSrv.Close)

	cfg := &config.AppConfig{
		RemotePushMimirURL: srv.URL,
		RemotePushWALDir:   t.TempDir(),
		TelemetryPort:      testTelemetryPort(t, metricsSrv.URL),
	}
	log := logger.NewAppSLogger(logger.Debug)

	ctx, cancel := context.WithCancel(context.Background())
	done := startMimirRemoteWrite(ctx, log, cfg)
	cancel()
	waitMimirDone(t, done, "done channel did not close within expected timeout")
}

func TestMimirRemoteWrite_walReplay(t *testing.T) {
	setTestToken(t)
	if testing.Short() {
		t.Skip("WAL replay test needs scrape + remote-write timing")
	}

	var pushCount atomic.Int32
	mimirDown := atomic.Bool{}
	mimirDown.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != mimirRemoteWritePath {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if mimirDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		pushCount.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: testMetricWalReplay})
	reg.MustRegister(g)
	g.Set(testGaugeValueReplay)

	metricsSrv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	t.Cleanup(metricsSrv.Close)

	walDir := t.TempDir()
	cfg := &config.AppConfig{
		RemotePushMimirURL: srv.URL,
		RemotePushWALDir:   walDir,
		TelemetryPort:      testTelemetryPort(t, metricsSrv.URL),
	}
	log := logger.NewAppSLogger(logger.Debug)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := startMimirRemoteWrite(ctx, log, cfg)

	require.Eventually(t, func() bool {
		return walHasData(t, walDir)
	}, walEventuallyTimeout, walEventuallyInterval, "expected WAL data while Mimir is down")

	mimirDown.Store(false)

	require.Eventually(t, func() bool {
		return pushCount.Load() > 0
	}, pushEventuallyTimeout, pushEventuallyInterval, "expected remote write after Mimir recovery")

	cancel()
	waitMimirDone(t, done, "done channel did not close")
}

func TestMimirRemoteWrite_processRestart(t *testing.T) {
	setTestToken(t)
	if testing.Short() {
		t.Skip("process restart test needs scrape + remote-write timing")
	}

	var pushCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == mimirRemoteWritePath {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
			pushCount.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: testMetricWalRestart})
	reg.MustRegister(g)
	g.Set(testGaugeValueRestart)

	metricsSrv := httptest.NewServer(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	t.Cleanup(metricsSrv.Close)

	walDir := t.TempDir()
	cfg := &config.AppConfig{
		RemotePushMimirURL: srv.URL,
		RemotePushWALDir:   walDir,
		TelemetryPort:      testTelemetryPort(t, metricsSrv.URL),
	}
	log := logger.NewAppSLogger(logger.Debug)

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := startMimirRemoteWrite(ctx1, log, cfg)
	require.Eventually(t, func() bool {
		return walHasData(t, walDir)
	}, walEventuallyTimeout, walEventuallyInterval, "expected WAL after first run")
	cancel1()
	waitMimirDone(t, done1, "first run did not shut down")

	pushAfterFirst := pushCount.Load()

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := startMimirRemoteWrite(ctx2, log, cfg)

	require.Eventually(t, func() bool {
		return pushCount.Load() > pushAfterFirst
	}, pushEventuallyTimeout, pushEventuallyInterval, "expected additional pushes after restart with same WAL dir")

	cancel2()
	waitMimirDone(t, done2, "second run did not shut down")
}

func TestInitMetrics_MimirDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	prevRegistry := CustomRegistry
	prevMimirDone := mimirDone
	t.Cleanup(func() {
		CustomRegistry = prevRegistry
		mimirDone = prevMimirDone
		oncer = sync.Once{}
	})
	CustomRegistry = nil
	mimirDone = nil
	oncer = sync.Once{}

	cfg := &config.AppConfig{
		TelemetryEnable:    true,
		RemotePushEnable:   true,
		RemotePushMimirURL: srv.URL,
		RemotePushWALDir:   t.TempDir(),
		GatewayID:          "test-gw",
		GatewayClusterID:   "test-cluster",
	}
	metricsSrv := httptest.NewServer(promhttp.HandlerFor(
		prometheus.NewRegistry(),
		promhttp.HandlerOpts{},
	))
	t.Cleanup(metricsSrv.Close)
	cfg.TelemetryPort = testTelemetryPort(t, metricsSrv.URL)
	setTestToken(t)
	log := logger.NewAppSLogger(logger.Debug)

	ctx, cancel := context.WithCancel(context.Background())
	ch := InitMetrics(ctx, log, cfg)
	require.NotNil(t, ch, "InitMetrics returned nil channel when RemotePushEnable=true")

	ch2 := InitMetrics(ctx, log, cfg)
	require.Equal(t, ch, ch2, "second InitMetrics call should return the same channel")

	cancel()
	waitMimirDone(t, ch, "mimirDone channel did not close within expected timeout")
}
