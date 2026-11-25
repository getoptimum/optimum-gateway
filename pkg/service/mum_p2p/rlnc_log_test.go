package mum_p2p

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

func TestToMumP2PConfigRelayCreditMultiplier(t *testing.T) {
	t.Parallel()

	cfg := toMumP2PConfig(&commonentities.OptimumConfig{
		MeshDegreeTarget:      16,
		MeshDegreeMin:         12,
		MeshDegreeMax:         24,
		HeartbeatMS:           250,
		RLNCK:                 16,
		MaxShardSize:          4096,
		RedundancyFraction:    1.5,
		RelayCreditMultiplier: 5,
	})
	require.Equal(t, 5, cfg.RLNC.RelayCreditMultiplier)
	require.Equal(t, 16, cfg.MeshD)
	require.Equal(t, 12, cfg.MeshDlo)
	require.Equal(t, 24, cfg.MeshDhi)
	require.Equal(t, 250, cfg.HeartbeatMS)
	require.Equal(t, uint32(16), cfg.RLNC.K)
	require.Equal(t, uint32(4096), cfg.RLNC.MaxShardSize)
	require.Equal(t, 1.5, cfg.RLNC.RedundancyFraction)
	require.NoError(t, cfg.Validate())
}
