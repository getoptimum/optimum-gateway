package config_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

// meshConfigLogMsg is the line an operator reads the node's mesh parameters off.
const meshConfigLogMsg = "mump2p mesh config"

// lockedBuffer collects log output written from the periodic logging goroutine
// as well as the test's own calls.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// lastMeshConfigLine returns the most recent mesh config line as decoded JSON.
func lastMeshConfigLine(t *testing.T, b *lockedBuffer) map[string]any {
	t.Helper()

	var last map[string]any
	for _, line := range strings.Split(b.String(), "\n") {
		if !strings.Contains(line, meshConfigLogMsg) {
			continue
		}
		entry := map[string]any{}
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == meshConfigLogMsg {
			last = entry
		}
	}
	require.NotNil(t, last, "no %q line was logged", meshConfigLogMsg)

	return last
}

// TestMeshConfigLogReportsTheRunningNodeNotTheServedConfig pins the log line to
// the node's own parameters once a node exists.
//
// The dynamic config rotator is seeded with the built-in defaults and fetches in
// the background, so a line logged from the served view at startup reports those
// defaults whatever an operator served. Read as the node's configuration it says
// the mesh is coding at a generation size the node never used, which is a
// difference no amount of staring at the mesh resolves.
func TestMeshConfigLogReportsTheRunningNodeNotTheServedConfig(t *testing.T) {
	buf := &lockedBuffer{}
	log := logger.InitLogger([]io.Writer{buf}, logger.Debug)

	// An empty cluster ID keeps the rotator from fetching, so the served view
	// stays at the seeded defaults for the length of the test.
	cfg := &config.AppConfig{}
	require.NoError(t, cfg.InitRuntime(t.Context(), log, "hoodi", "gw-test", "hermes", "org-test"))

	cfg.LogConfigState()
	served := lastMeshConfigLine(t, buf)
	require.Equal(t, entities.RLNCParamsSourceDynamicConfig, served["source"])
	require.InDelta(t, float64(config.DefaultShardFactor), served["shard_factor"], 1e-9)
	require.InDelta(t, float64(config.DefaultMeshDegreeMax), served["mesh_degree_max"], 1e-9)

	cfg.SetEffectiveRLNCSource(func() (entities.RLNCParams, bool) {
		return entities.RLNCParams{
			ShardFactor:          16,
			MaxShardSize:         1136,
			RedundancyFraction:   2.5,
			ForwardThreshold:     0.75,
			ForwardRankThreshold: 12,
			MeshDegreeTarget:     6,
			MeshDegreeMin:        4,
			MeshDegreeMax:        8,
		}, true
	})

	cfg.LogConfigState()
	running := lastMeshConfigLine(t, buf)
	require.Equal(t, entities.RLNCParamsSourceNode, running["source"])
	require.InDelta(t, 16.0, running["shard_factor"], 1e-9)
	require.InDelta(t, 8.0, running["mesh_degree_max"], 1e-9)
	require.InDelta(t, 12.0, running["forward_rank_threshold"], 1e-9)
	require.InDelta(t, 1136.0, running["max_shard_size"], 1e-9)
	require.InDelta(t, 0.75, running["forward_shard_threshold"], 1e-9)
}
