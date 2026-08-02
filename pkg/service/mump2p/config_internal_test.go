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

// The protocol node ID becomes the mump2p.node_id span attribute, which per-node
// analysis keys on, so it has to distinguish gateways. The cluster ID is
// identical across the fleet and would collapse all of them into one node.
func TestBaseNodeConfigNodeIDIsPerGateway(t *testing.T) {
	const clusterID = "bench-cluster"

	first := baseNodeConfig(&Config{ClusterID: clusterID, GatewayID: "bench-gw-0", ListenPort: 4321})
	second := baseNodeConfig(&Config{ClusterID: clusterID, GatewayID: "bench-gw-1", ListenPort: 4321})

	require.Equal(t, "bench-gw-0", first.ID)
	require.Equal(t, "bench-gw-1", second.ID)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, clusterID, first.ClusterID, "the cluster id still travels as the cluster id")
	require.NoError(t, first.Validate())

	// Without a gateway id there is nothing better than the cluster id here; the
	// node resolves it to its peer id at startup instead.
	fallback := baseNodeConfig(&Config{ClusterID: clusterID, ListenPort: 4321})
	require.Equal(t, clusterID, fallback.ID)
}

// Role and ProtocolVersion are validate:"required" and the gateway never sets
// them, so the protocol defaults are what keeps the dynamic-config path valid and
// the mump2p.role span attribute meaningful.
func TestBaseNodeConfigKeepsRequiredIdentityDefaults(t *testing.T) {
	got := baseNodeConfig(&Config{ClusterID: "bench-cluster", GatewayID: "bench-gw-0", ListenPort: 4321})

	require.Equal(t, mp2pconfig.RoleBoth, got.Role)
	require.Equal(t, "v2", got.ProtocolVersion)
	require.NoError(t, got.Validate())
}

func TestOTelConfigMapping(t *testing.T) {
	defaults := mp2pconfig.DefaultOTelConfig()

	t.Run("DefaultsOff", func(t *testing.T) {
		got := (&Config{}).otel()
		require.False(t, got.Enable)
		require.Empty(t, got.Endpoint)
		require.InEpsilon(t, defaults.SampleRatio, got.SampleRatio, 1e-6)
	})

	t.Run("SetFieldsOverride", func(t *testing.T) {
		got := (&Config{
			OTelEnable:      true,
			OTelEndpoint:    "otel-collector:4318",
			OTelInsecure:    true,
			OTelSampleRatio: 0.25,
		}).otel()
		require.True(t, got.Enable)
		require.Equal(t, "otel-collector:4318", got.Endpoint)
		require.True(t, got.Insecure)
		require.InEpsilon(t, 0.25, got.SampleRatio, 1e-6)
	})
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

// TestToNodeConfigSizesTheCoderForTheDatagramPath pins the shard size the coder
// is built with, which is the one field of the resolved config no other test
// covers and the one a datagram deployment cannot be wrong about.
//
// The coder and the router are built from this same config, so the stream-path
// default reaching the coder means every symbol is sized for a transport the
// node is not using: a payload splits into an order of magnitude more chunks
// than the datagram budget calls for, and a message only reassembles once every
// chunk independently reaches full rank.
func TestToNodeConfigSizesTheCoderForTheDatagramPath(t *testing.T) {
	cfg := &Config{
		ClusterID:      "test-cluster",
		ListenPort:     4321,
		DatagramEnable: true,
		Rotator:        newRotator(t, &commonentities.OptimumConfig{}),
	}

	got, err := toNodeConfig(cfg)
	require.NoError(t, err)
	require.NoError(t, got.Validate())

	require.NotEqual(t, uint32(mp2pconfig.DefaultMaxShardSize), got.MaxShardSize,
		"the coder is still sharding at the stream-path default with the datagram path enabled")
	require.Greater(t, got.MaxShardSize, uint32(mp2pconfig.DefaultMaxShardSize))
}

// The stream path must be untouched by all of this: a deployment that does not
// enable the datagram transport has to code exactly as it did before.
func TestToNodeConfigLeavesTheStreamPathShardSizeAlone(t *testing.T) {
	cfg := &Config{
		ClusterID:  "test-cluster",
		ListenPort: 4321,
		Rotator:    newRotator(t, &commonentities.OptimumConfig{}),
	}

	got, err := toNodeConfig(cfg)
	require.NoError(t, err)

	require.Equal(t, uint32(mp2pconfig.DefaultMaxShardSize), got.MaxShardSize)
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
