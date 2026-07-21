package test_utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

func GetTestConfig(t *testing.T) *config.AppConfig {
	t.Helper()

	cfgRaw := &config.AppConfig{
		LogLevel:               string(logger.Debug),
		IdentityLibP2PDir:      getEnv("OPT_TEST_IDENTITY_DIR", t.TempDir),
		IdentityMumP2PDir:      getEnv("OPT_IDENTITY_MUMP2P_DIR", t.TempDir),
		AgentLibP2PPort:        getEnvInt("OPT_TEST_GATEWAY_PORT", test_utils.GetFreePortT(t)),
		AgentMumP2PPort:        getEnvInt("OPT_AGENT_MUMP2P_PORT", test_utils.GetFreePortT(t)),
		GatewayClusterID:       getEnvStr("OPT_CLUSTER_ID", "optimum_hoodi_v0_2"),
		GatewayID:              getEnvStr("OPT_GATEWAY_ID", "local_test_srv_"),
		TelemetryPort:          48123,
		TelemetryEnable:        true,
		PropagationEnabledRaw:  true, // Enable propagation in tests to ensure all messages are forwarded
		RemoteBootstrapURL:     getEnvStr("OPT_REMOTE_BOOTSTRAP_URL", "dev-bootstrap.getoptimum.io"),
		JWKSCachePath:          getEnvStr("OPT_JWKS_CACHE_PATH", ""),
		JWKSRefreshIntervalSec: 3600,
		APIKey:                 getEnvStr("OPT_API_KEY", ""),
		EnableAuth:             getEnvStr("OPT_ENABLE_AUTH", "false") == "true",
		RemoteAuthURL:          getEnvStr("OPT_REMOTE_AUTH_URL", "https://dev-auth.getoptimum.io"),
	}
	if val := getEnvStr("OPT_DIRECT_CL_PEERS", ""); val != "" {
		cfgRaw.DirectCLPeers = strings.Split(val, ",")
	}
	return LoadTestConfig(t, cfgRaw)
}

// LoadTestConfig loads cfgRaw through the same YAML round-trip as GetTestConfig.
func LoadTestConfig(t *testing.T, cfgRaw *config.AppConfig) *config.AppConfig {
	t.Helper()

	data, err := yaml.Marshal(cfgRaw) //nolint:gosec // it unit tests
	require.NoError(t, err)
	locatePath := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(locatePath, data, 0o600))

	cfg, err := config.LoadConfig(locatePath)
	require.NoError(t, err)
	return cfg
}

func getEnv(key string, defaultValueFn func() string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValueFn()
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		fmt.Printf("Error converting environment variable %s to int: %v\n", key, err)
		return defaultValue
	}
	return intValue
}

func getEnvStr(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
