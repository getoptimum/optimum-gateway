package gossipsub_gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// appConfigWithRotator returns a config whose rotator serves only what tests feed it:
// the bootstrap endpoint answers 404, so no fetched config can race the served values.
func appConfigWithRotator(t *testing.T) *config.AppConfig {
	t.Helper()

	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	cnt := test_utils.GetClean(t)
	cfg := &config.AppConfig{
		AgentMumP2PPort:    33213,
		GatewayClusterID:   "test-cluster",
		RemoteBootstrapURL: srv.URL,
	}
	require.NoError(t, cfg.InitRuntime(cnt.Ctx, cnt.Log, "hoodi", "test-gateway", "", ""))
	return cfg
}

func TestBuildMumP2PConfigUsesServedValues(t *testing.T) {
	cfg := appConfigWithRotator(t)
	cfg.GetDCRotator().RenewConfig(&commonentities.DynamicConfig{
		RandomMessageSize:        1024,
		ShardFactor:              16,
		PublisherShardMultiplier: 2.5,
		ForwardShardThreshold:    0, // zero is a valid setting, not "unset"
		MeshDegreeTarget:         9,
		MeshDegreeMin:            7,
		MeshDegreeMax:            21,
	})

	optCfg := buildMumP2PConfig(cfg, []string{"peer-a"})

	require.Equal(t, int64(1024), optCfg.RandomMessageSize)
	require.Equal(t, 16, optCfg.ShardFactor)
	require.InDelta(t, 2.5, optCfg.PublisherShardMultiplier, 1e-6)
	require.Zero(t, optCfg.ForwardShardThreshold)
	require.Equal(t, 9, optCfg.MeshDegreeTarget)
	require.Equal(t, 7, optCfg.MeshDegreeMin)
	require.Equal(t, 21, optCfg.MeshDegreeMax)

	require.Equal(t, cfg.GetDCRotator(), optCfg.Rotator)
	require.Equal(t, config.DefaultMaxMessageSize, optCfg.MaxMessageSize)
	require.Equal(t, cfg.AgentMumP2PPort, optCfg.ListenPort)
	require.Equal(t, cfg.GatewayClusterID, optCfg.ClusterID)
	require.Equal(t, []string{"peer-a"}, optCfg.BootstrapPeers)
}

func TestBuildMumP2PConfigDefaultsWhenNothingServed(t *testing.T) {
	// No InitRuntime: the rotator is absent, so the documented defaults must stand.
	cfg := &config.AppConfig{AgentMumP2PPort: 33213, GatewayClusterID: "test-cluster"}

	optCfg := buildMumP2PConfig(cfg, nil)

	require.Nil(t, optCfg.Rotator)
	require.Equal(t, config.DefaultRandomMessageSize, optCfg.RandomMessageSize)
	require.Equal(t, int(config.DefaultShardFactor), optCfg.ShardFactor)
	require.InDelta(t, config.DefaultPublisherShardMultiplier, optCfg.PublisherShardMultiplier, 1e-6)
	require.InDelta(t, config.DefaultForwardShardThreshold, optCfg.ForwardShardThreshold, 1e-6)
	require.Equal(t, int(config.DefaultMeshDegreeTarget), optCfg.MeshDegreeTarget)
	require.Equal(t, int(config.DefaultMeshDegreeMin), optCfg.MeshDegreeMin)
	require.Equal(t, int(config.DefaultMeshDegreeMax), optCfg.MeshDegreeMax)
}

// The coder is out of process, so its shared-memory handle has to reach the node:
// an unset handle here would attach to the wrong sidecar rather than fail loudly.
func TestBuildMumP2PConfigCarriesTheCoderSHMHandle(t *testing.T) {
	cfg := &config.AppConfig{
		AgentMumP2PPort:  33213,
		GatewayClusterID: "test-cluster",
		SHMName:          "gateway-coder",
		SHMLanes:         4,
	}

	optCfg := buildMumP2PConfig(cfg, nil)

	require.Equal(t, "gateway-coder", optCfg.SHMName)
	require.Equal(t, 4, optCfg.SHMLanes)
}

// TestBuildMumP2PConfigDatagramFlag proves the data plane is off unless the
// operator turns it on, and that the settings reach the node when they do.
func TestBuildMumP2PConfigDatagramFlag(t *testing.T) {
	cfg := &config.AppConfig{GatewayClusterID: "cluster", AgentMumP2PPort: 33213}
	require.False(t, buildMumP2PConfig(cfg, nil).DatagramEnable)

	cfg.DatagramEnable = true
	cfg.DatagramListenAddr = "127.0.0.1:4444"
	cfg.DatagramMaxPayload = 900

	got := buildMumP2PConfig(cfg, nil)
	require.True(t, got.DatagramEnable)
	require.Equal(t, "127.0.0.1:4444", got.DatagramListenAddr)
	require.Equal(t, 900, got.DatagramMaxPayload)
}

// TestBuildMumP2PConfigOTelSettings pins the tracing settings to the node
// config. AppConfig validates the endpoint whenever tracing is enabled, so a
// gateway that drops these here starts cleanly, logs nothing and exports no
// spans: the only symptom is an empty collector.
func TestBuildMumP2PConfigOTelSettings(t *testing.T) {
	cfg := &config.AppConfig{GatewayClusterID: "cluster", AgentMumP2PPort: 33213}
	require.False(t, buildMumP2PConfig(cfg, nil).OTelEnable)

	cfg.OTelEnable = true
	cfg.OTelEndpoint = "otel-collector:4318"
	cfg.OTelInsecure = true
	cfg.OTelSampleRatio = 1.0

	got := buildMumP2PConfig(cfg, nil)
	require.True(t, got.OTelEnable)
	require.Equal(t, "otel-collector:4318", got.OTelEndpoint)
	require.True(t, got.OTelInsecure)
	require.InDelta(t, 1.0, got.OTelSampleRatio, 1e-9)
}
