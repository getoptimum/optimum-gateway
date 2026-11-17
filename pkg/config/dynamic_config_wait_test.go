package config

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

func TestWaitDynamicConfigRefusesWrongVersionAndTimeout(t *testing.T) {
	log := logger.NewAppSLogger(logger.Error)

	t.Run("wrong service_version", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"service_version":"gateway-v1.3.4"}`))
		}))
		t.Cleanup(srv.Close)

		cfg := &AppConfig{
			Version:            "v9.9.9",
			GatewayClusterID:   "optimum_ethereum_hoodi_v0_1",
			RemoteBootstrapURL: srv.URL,
		}
		require.NoError(t, cfg.InitRuntime(t.Context(), log, "hoodi", "", "", "", nil))
		_, err := cfg.WaitDynamicConfig(t.Context())
		require.ErrorContains(t, err, `dynamic config service_version "gateway-v1.3.4", want "gateway-v9.9.9"`)
		require.Nil(t, cfg.StartedOptimumConfig())
	})

	t.Run("fetch timeout", func(t *testing.T) {
		prev := dynamicConfigFetchTimeout
		dynamicConfigFetchTimeout = 20 * time.Millisecond
		t.Cleanup(func() { dynamicConfigFetchTimeout = prev })

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no row", http.StatusNotFound)
		}))
		t.Cleanup(srv.Close)

		cfg := &AppConfig{
			Version:            "v9.9.9",
			GatewayClusterID:   "optimum_ethereum_hoodi_v0_1",
			RemoteBootstrapURL: srv.URL,
		}
		require.NoError(t, cfg.InitRuntime(t.Context(), log, "hoodi", "", "", "", nil))
		_, err := cfg.WaitDynamicConfig(t.Context())
		require.ErrorContains(t, err, "timed out waiting for dynamic config")
		require.Nil(t, cfg.StartedOptimumConfig())
	})
}
