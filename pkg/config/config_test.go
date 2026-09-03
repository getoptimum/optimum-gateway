package config_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/version"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

const (
	testLibP2PDir = "/tmp/lib"
	testMumP2PDir = "/tmp/mump2p"
)

// helper to create a temporary config file
func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "app_conf.yml")
	require.NoError(t, os.WriteFile(tmp, []byte(content), 0o600))
	return tmp
}

func TestLoadConfig_FromFile(t *testing.T) {
	// given
	confPath := writeTempConfig(t, `
log_level: "info"
identity_libp2p_dir: "/tmp/lib"
identity_mump2p_dir: "/tmp/mump2p"
agent_lib_p2p_port: 4000
agent_mump2p_port: 4001
gateway_id: "gw-1"
gateway_cluster_id: "gw-cluster"
telemetry_enable: true
telemetry_port: 9999`)

	// when
	cfg, err := config.LoadConfig(confPath)
	require.NoError(t, err)

	// then
	require.Equal(t, "info", cfg.LogLevel)
	require.Equal(t, 4000, cfg.AgentLibP2PPort)
	require.True(t, cfg.TelemetryEnable)
	require.Equal(t, "127.0.0.1:6060", cfg.PProfAddr)
	require.Equal(t, version.GetVersion(), cfg.Version)
	require.Equal(t, version.GetCommitHash(), cfg.CommitHash)
}

func TestLoadConfig_FileNotFound_ShouldFail(t *testing.T) {
	_, err := config.LoadConfig("/does/not/exist.yml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to read config file")
}

func TestLoadConfig_FromEnvOnly(t *testing.T) {
	t.Setenv("OPT_LOG_LEVEL", "debug")
	t.Setenv("OPT_AGENT_LIB_P2P_PORT", "5000")
	t.Setenv("OPT_AGENT_MUMP2P_PORT", "5001")
	t.Setenv("OPT_IDENTITY_LIBP2P_DIR", "./libid")
	t.Setenv("OPT_IDENTITY_MUMP2P_DIR", "./mump2pid")
	t.Setenv("OPT_GATEWAY_ID", "gw-env")
	t.Setenv("OPT_GATEWAY_CLUSTER_ID", "gw-cluster-env")
	t.Setenv("OPT_ENABLE_TELEMETRY", "true")
	t.Setenv("OPT_TELEMETRY_PORT", "8888")

	cfg, err := config.LoadConfig("")
	require.NoError(t, err)

	require.Equal(t, 5000, cfg.AgentLibP2PPort)
	require.True(t, cfg.TelemetryEnable)
	require.Equal(t, "gw-env", cfg.GatewayID)
	require.Equal(t, 8888, cfg.TelemetryPort)
	require.Equal(t, "127.0.0.1:6060", cfg.PProfAddr)
	require.Equal(t, version.GetVersion(), cfg.Version)
	require.Equal(t, version.GetCommitHash(), cfg.CommitHash)
}

// JWKS refresh default was lowered from 24h to 1h to tighten the
// revocation-propagation window; a regression that flipped it back to
// 86400 would silently widen the gap. Pin the default so the change
// doesn't decay.
func TestLoadConfig_JWKSRefreshDefault_Is1Hour(t *testing.T) {
	// Per-test temp dirs so parallel runs (or `-count=N`) don't share
	// state through fixed relative paths.
	tmp := t.TempDir()
	t.Setenv("OPT_IDENTITY_LIBP2P_DIR", filepath.Join(tmp, "libid"))
	t.Setenv("OPT_IDENTITY_MUMP2P_DIR", filepath.Join(tmp, "mump2pid"))
	t.Setenv("OPT_GATEWAY_CLUSTER_ID", "gw-cluster-env")
	t.Setenv("OPT_TELEMETRY_PORT", "8888")

	cfg, err := config.LoadConfig("")
	require.NoError(t, err)
	require.Equal(t, 3600, cfg.JWKSRefreshIntervalSec,
		"JWKS refresh default must stay at 1h — see Finding #9 (24h gave a too-wide revocation window)")
}

func TestLoadConfig_EnvOverridesYAML(t *testing.T) {
	// given
	confPath := writeTempConfig(t, `
identity_libp2p_dir: "./lib"
identity_mump2p_dir: "./mump2p"
agent_lib_p2p_port: 4000
agent_mump2p_port: 4001
gateway_id: abc
gateway_cluster_id: cluster-1
`)

	// override gateway_id from env
	t.Setenv("OPT_GATEWAY_ID", "from-env")

	// when
	cfg, err := config.LoadConfig(confPath)
	require.NoError(t, err)

	// then env should win over YAML
	require.Equal(t, "from-env", cfg.GatewayID)
}

func TestValidate_PProfEnabled_EmptyAddr(t *testing.T) {
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryPort:     48123,
		GatewayID:         "gw-pprof",
		GatewayClusterID:  "cluster",
		EnablePProf:       true,
		PProfAddr:         "  ",
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "pprof_addr")
}

func TestValidate_PProfEnabled_InvalidAddr(t *testing.T) {
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryPort:     48123,
		GatewayID:         "gw-pprof",
		GatewayClusterID:  "cluster",
		EnablePProf:       true,
		PProfAddr:         "not-a-valid-tcp-addr",
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid pprof_addr")
}

func TestValidate_PProfEnabled_ValidAddr(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryPort:     48123,
		GatewayID:         "gw-pprof",
		GatewayClusterID:  "cluster",
		EnablePProf:       true,
		PProfAddr:         addr,
	}
	require.NoError(t, cfg.Validate())
}

func TestValidate_MissingFields(t *testing.T) {
	cfg := &config.AppConfig{
		// empty on purpose
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "OPT_IDENTITY_LIBP2P_DIR")
}

func TestValidate_InvalidPorts(t *testing.T) {
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   -1,
		AgentMumP2PPort:   70000,
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be between 1 and 65535")
}

func TestValidate_MissingClusterID(t *testing.T) {
	// GatewayID is no longer required (JWT subject overrides), but
	// GatewayClusterID still must be supplied by config.
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryEnable:   true,
		TelemetryPort:     48123,
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "OPT_GATEWAY_CLUSTER_ID")
}

func TestValidate_TelemetryPortRequiredWhenTelemetryDisabled(t *testing.T) {
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		GatewayClusterID:  "optimum_hoodi_v0_3",
		TelemetryEnable:   false,
		TelemetryPort:     0,
	}
	err := cfg.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "OPT_TELEMETRY_PORT")
}

func TestInitRuntime_GatewayIDFromSub(t *testing.T) {
	cfg := &config.AppConfig{
		GatewayID:        "yaml-fallback",
		GatewayClusterID: "optimum_hoodi_v0_3",
	}
	log := logger.NewAppSLogger(logger.Debug)

	require.NoError(t, cfg.InitRuntime(t.Context(), log, "560048", "ag_test_sub", "hermes", "org-123"))
	require.Equal(t, "ag_test_sub", cfg.GatewayID)
	require.Equal(t, "hermes", cfg.GatewayType)
	require.Equal(t, "org-123", cfg.OrgID)

	require.NoError(t, cfg.InitRuntime(t.Context(), log, "hoodi", "", "", ""))
	require.Equal(t, "ag_test_sub", cfg.GatewayID, "empty sub must not overwrite existing gateway id")
	require.Equal(t, "hermes", cfg.GatewayType, "empty type must not overwrite existing gateway type")
}

func TestLoadConfig_BasicFieldsFromYAML(t *testing.T) {
	confYml := `
log_level: debug
identity_libp2p_dir: /tmp/libp2p
identity_mump2p_dir: /tmp/mump2p
agent_lib_p2p_port: 33212
agent_mump2p_port: 43213
telemetry_enable: true
telemetry_port: 48123
gateway_cluster_id: optimum_hoodi_v0_1
gateway_id: local-dockerized
`
	confPath := writeTempConfig(t, confYml)

	cfg, err := config.LoadConfig(confPath)
	require.NoError(t, err)

	require.Equal(t, "debug", cfg.LogLevel)
	require.Equal(t, 33212, cfg.AgentLibP2PPort)
	require.Equal(t, 43213, cfg.AgentMumP2PPort)
	require.True(t, cfg.TelemetryEnable)
	require.Equal(t, "local-dockerized", cfg.GatewayID)
	require.Equal(t, "optimum_hoodi_v0_1", cfg.GatewayClusterID)
}

// The stream is off by default, and when enabled auth may be disabled only on
// a loopback bind (ADR-0011 exposure rule).
func TestStreamValidation(t *testing.T) {
	base := func(t *testing.T) {
		t.Helper()
		t.Setenv("OPT_IDENTITY_LIBP2P_DIR", "./libid")
		t.Setenv("OPT_IDENTITY_MUMP2P_DIR", "./mump2pid")
		t.Setenv("OPT_AGENT_LIB_P2P_PORT", "5000")
		t.Setenv("OPT_AGENT_MUMP2P_PORT", "5001")
		t.Setenv("OPT_GATEWAY_CLUSTER_ID", "gw-cluster")
		t.Setenv("OPT_TELEMETRY_PORT", "8888")
	}

	t.Run("off by default", func(t *testing.T) {
		base(t)
		cfg, err := config.LoadConfig("")
		require.NoError(t, err)
		require.False(t, cfg.StreamEnable)
		require.True(t, cfg.StreamRequireAuth)
		// Loopback by default: enabling the stream must not, on its own,
		// publish a consumer feed on every interface (ADR-0011 §Exposure).
		require.Equal(t, "127.0.0.1:9600", cfg.StreamAddr)
		require.Equal(t, "127.0.0.1:9601", cfg.StreamGRPCAddr)
	})

	t.Run("auth off on default bind allowed", func(t *testing.T) {
		base(t)
		t.Setenv("OPT_STREAM_ENABLE", "true")
		t.Setenv("OPT_STREAM_REQUIRE_AUTH", "false")
		_, err := config.LoadConfig("")
		require.NoError(t, err)
	})

	t.Run("auth off on loopback allowed", func(t *testing.T) {
		base(t)
		t.Setenv("OPT_STREAM_ENABLE", "true")
		t.Setenv("OPT_STREAM_REQUIRE_AUTH", "false")
		t.Setenv("OPT_STREAM_ADDR", "127.0.0.1:9600")
		t.Setenv("OPT_STREAM_GRPC_ADDR", "localhost:9601")
		_, err := config.LoadConfig("")
		require.NoError(t, err)
	})

	t.Run("auth off on exposed bind rejected", func(t *testing.T) {
		base(t)
		t.Setenv("OPT_STREAM_ENABLE", "true")
		t.Setenv("OPT_STREAM_REQUIRE_AUTH", "false")
		t.Setenv("OPT_STREAM_ADDR", "0.0.0.0:9600")
		_, err := config.LoadConfig("")
		require.ErrorContains(t, err, "stream_require_auth=true")
	})

	t.Run("stream_only without stream_enable rejected", func(t *testing.T) {
		base(t)
		t.Setenv("OPT_STREAM_ONLY", "true")
		_, err := config.LoadConfig("")
		require.ErrorContains(t, err, "stream_only requires stream_enable")
	})

	t.Run("stream_only with stream_enable allowed", func(t *testing.T) {
		base(t)
		t.Setenv("OPT_STREAM_ENABLE", "true")
		t.Setenv("OPT_STREAM_ONLY", "true")
		cfg, err := config.LoadConfig("")
		require.NoError(t, err)
		require.True(t, cfg.StreamOnly)
	})
}

func TestValidate_AnnounceIP_Valid(t *testing.T) {
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryPort:     48123,
		GatewayClusterID:  "cluster",
		AnnounceIP:        "203.0.113.10",
	}
	require.NoError(t, cfg.Validate())
	require.Equal(t, "203.0.113.10", cfg.AnnounceIP)
}

func TestValidate_AnnounceIP_Empty_NoOp(t *testing.T) {
	// Not set at all - Validate should neither error nor touch the field.
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryPort:     48123,
		GatewayClusterID:  "cluster",
	}
	require.NoError(t, cfg.Validate())
	require.Empty(t, cfg.AnnounceIP)
}

func TestValidate_AnnounceIP_Invalid(t *testing.T) {
	cases := []string{
		"not-an-ip",
		"999.999.999.999",
		"",                     // handled separately by the empty-string no-op case, but a whitespace-only value is not empty and should still fail
		"2001:db8::1",          // a genuine IPv6 address, not IPv4
		"announce.example.com", // hostname, not an address
		"203.0.113.10:8080",    // host:port, not a bare address
	}
	for _, in := range cases {
		if in == "" {
			continue // covered by TestValidate_AnnounceIP_Empty_NoOp
		}
		cfg := &config.AppConfig{
			IdentityLibP2PDir: testLibP2PDir,
			IdentityMumP2PDir: testMumP2PDir,
			AgentLibP2PPort:   33212,
			AgentMumP2PPort:   33213,
			TelemetryPort:     48123,
			GatewayClusterID:  "cluster",
			AnnounceIP:        in,
		}
		err := cfg.Validate()
		require.Errorf(t, err, "expected %q to be rejected as an invalid IPv4 address", in)
		require.Contains(t, err.Error(), "OPT_ANNOUNCE_IP")
	}
}

// TestValidate_AnnounceIP_NormalizesIPv4MappedIPv6 guards the specific bug
// CodeRabbit flagged on PR #108: net.ParseIP("::ffff:203.0.113.10").To4()
// is non-nil (Go's net package treats IPv4-mapped IPv6 addresses as valid
// IPv4), so a bare "does To4() succeed" check accepts this text - but the
// raw string "::ffff:203.0.113.10" is not valid inside an
// "/ip4/<addr>/tcp/<port>" multiaddr. Validate must normalize to the
// canonical dotted-decimal form before the value is used anywhere else.
func TestValidate_AnnounceIP_NormalizesIPv4MappedIPv6(t *testing.T) {
	cfg := &config.AppConfig{
		IdentityLibP2PDir: testLibP2PDir,
		IdentityMumP2PDir: testMumP2PDir,
		AgentLibP2PPort:   33212,
		AgentMumP2PPort:   33213,
		TelemetryPort:     48123,
		GatewayClusterID:  "cluster",
		AnnounceIP:        "::ffff:203.0.113.10",
	}
	require.NoError(t, cfg.Validate())
	require.Equal(t, "203.0.113.10", cfg.AnnounceIP,
		"AnnounceIP must be normalized to dotted-decimal, not left as IPv4-mapped IPv6 text")
}

func TestLoadConfig_AnnounceIP_FromEnv(t *testing.T) {
	t.Setenv("OPT_IDENTITY_LIBP2P_DIR", "./libid")
	t.Setenv("OPT_IDENTITY_MUMP2P_DIR", "./mump2pid")
	t.Setenv("OPT_AGENT_LIB_P2P_PORT", "5000")
	t.Setenv("OPT_AGENT_MUMP2P_PORT", "5001")
	t.Setenv("OPT_GATEWAY_CLUSTER_ID", "gw-cluster")
	t.Setenv("OPT_TELEMETRY_PORT", "8888")
	t.Setenv("OPT_ANNOUNCE_IP", "198.51.100.7")
	cfg, err := config.LoadConfig("")
	require.NoError(t, err)
	require.Equal(t, "198.51.100.7", cfg.AnnounceIP)
}
