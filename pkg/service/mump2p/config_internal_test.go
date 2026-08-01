package mump2p

import (
	"testing"

	"github.com/stretchr/testify/require"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	cfgpkg "github.com/getoptimum/optimum-gateway/pkg/config"
)

func newRotator(t *testing.T, served *commonentities.OptimumConfig) *commonconfig.Rotator {
	t.Helper()

	return commonconfig.NewConfigRotator(
		t.Context(),
		logger.NewAppSLogger(logger.Debug),
		served,
		"hoodi",
		"test-cluster",
		func(*commonentities.DynamicConfig) {},
	)
}

func TestToNodeConfigMapsServedValues(t *testing.T) {
	cfg := &Config{
		ClusterID:  "test-cluster",
		ListenPort: 4321,
		Rotator: newRotator(t, &commonentities.OptimumConfig{
			MeshDegreeTarget:         cfgpkg.DefaultMeshDegreeTarget,
			MeshDegreeMin:            cfgpkg.DefaultMeshDegreeMin,
			MeshDegreeMax:            cfgpkg.DefaultMeshDegreeMax,
			ShardFactor:              cfgpkg.DefaultShardFactor,
			PublisherShardMultiplier: cfgpkg.DefaultPublisherShardMultiplier,
			ForwardShardThreshold:    cfgpkg.DefaultForwardShardThreshold,
		}),
	}

	got, err := toNodeConfig(cfg)
	require.NoError(t, err)

	require.Equal(t, "test-cluster", got.ClusterID)
	require.Equal(t, 4321, got.Port)
	require.Equal(t, mp2pconfig.TransportQUIC, got.Transport)
	require.Equal(t, heartbeatMS, got.HeartbeatMS)
	require.Equal(t, historyLength, got.HistoryLength)
	require.Equal(t, historyGossip, got.HistoryGossip)

	require.Equal(t, int(cfgpkg.DefaultMeshDegreeTarget), got.MeshD)
	require.Equal(t, int(cfgpkg.DefaultMeshDegreeMin), got.MeshDlo)
	require.Equal(t, int(cfgpkg.DefaultMeshDegreeMax), got.MeshDhi)

	require.Equal(t, uint32(cfgpkg.DefaultShardFactor), got.K)
	require.InEpsilon(t, float64(cfgpkg.DefaultPublisherShardMultiplier), got.RedundancyFraction, 1e-6)
	require.InEpsilon(t, float64(cfgpkg.DefaultForwardShardThreshold), got.ForwardingThresholdFraction, 1e-6)
}

// A served mesh target without an explicit min/max widens the window around it.
func TestToNodeConfigDerivesMeshWindowFromTarget(t *testing.T) {
	cfg := &Config{
		ListenPort: 4321,
		Rotator:    newRotator(t, &commonentities.OptimumConfig{MeshDegreeTarget: 8}),
	}

	got, err := toNodeConfig(cfg)
	require.NoError(t, err)

	require.Equal(t, 8, got.MeshD)
	require.Equal(t, 7, got.MeshDlo)
	require.Equal(t, 8+meshDegreeHiOffset, got.MeshDhi)
}

// Zeros mean "not served", so the protocol defaults must survive them rather than
// being written through as invalid mesh degrees or a zero generation size.
func TestToNodeConfigKeepsDefaultsForUnservedValues(t *testing.T) {
	cfg := &Config{
		ListenPort: 4321,
		Rotator:    newRotator(t, &commonentities.OptimumConfig{}),
	}
	defaults := mp2pconfig.DefaultGossipSubConfig()

	got, err := toNodeConfig(cfg)
	require.NoError(t, err)

	require.Equal(t, defaults.MeshD, got.MeshD)
	require.Equal(t, defaults.MeshDlo, got.MeshDlo)
	require.Equal(t, defaults.MeshDhi, got.MeshDhi)
	require.Equal(t, defaults.K, got.K)
	require.InEpsilon(t, defaults.RedundancyFraction, got.RedundancyFraction, 1e-6)
}

// A served mesh window the protocol rejects must not block startup: the node falls
// back to the defaults and reports why.
func TestToNodeConfigFallsBackOnRejectedServedValues(t *testing.T) {
	cfg := &Config{
		ClusterID:  "test-cluster",
		ListenPort: 4321,
		Rotator:    newRotator(t, &commonentities.OptimumConfig{MeshDegreeMin: 40}),
	}
	defaults := mp2pconfig.DefaultGossipSubConfig()

	got, err := toNodeConfig(cfg)

	require.ErrorContains(t, err, "dynamic mump2p config rejected")
	require.Equal(t, defaults.MeshDlo, got.MeshDlo)
	require.NoError(t, got.Validate())
}

func TestSharedMemoryOverrides(t *testing.T) {
	defaults := mp2pconfig.DefaultSharedMemoryConfig()

	empty := (&Config{}).sharedMemory()
	require.Equal(t, defaults.SHMName, empty.SHMName)
	require.Equal(t, defaults.SHMLanes, empty.SHMLanes)

	overridden := (&Config{SHMName: "gateway-coder", SHMLanes: 4}).sharedMemory()
	require.Equal(t, "gateway-coder", overridden.SHMName)
	require.Equal(t, 4, overridden.SHMLanes)
}

// TestDatagramFlagDefaultsOff pins the feature flag: the datagram data plane
// binds a UDP socket and moves mesh traffic onto per-peer keys, so a config that
// says nothing about it must leave it off.
func TestDatagramFlagDefaultsOff(t *testing.T) {
	cfg := &Config{ClusterID: "test-cluster", ListenPort: 4321, Rotator: newRotator(t, &commonentities.OptimumConfig{})}

	got, err := toNodeConfig(cfg)
	require.NoError(t, err)
	require.False(t, got.Datagram.Enable)
}

func TestDatagramConfigMapping(t *testing.T) {
	defaults := mp2pconfig.DefaultDatagramConfig()

	t.Run("UnsetFieldsKeepProtocolDefaults", func(t *testing.T) {
		got := (&Config{DatagramEnable: true}).datagram()
		require.True(t, got.Enable)
		require.Equal(t, defaults.ListenAddr, got.ListenAddr)
		require.Equal(t, defaults.MaxPayload, got.MaxPayload)
	})

	t.Run("SetFieldsOverride", func(t *testing.T) {
		got := (&Config{
			DatagramEnable:     true,
			DatagramListenAddr: "127.0.0.1:4444",
			DatagramMaxPayload: 900,
		}).datagram()
		require.True(t, got.Enable)
		require.Equal(t, "127.0.0.1:4444", got.ListenAddr)
		require.Equal(t, 900, got.MaxPayload)
	})
}
