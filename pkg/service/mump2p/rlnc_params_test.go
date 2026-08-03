package mump2p_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	commontest "github.com/getoptimum/optimum-common/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// servedShardFactor and servedMeshDegreeMax are deliberately not the package
// defaults (4 and 12): a node reading the built-in defaults instead of the
// served configuration must fail this test rather than pass by coincidence.
const (
	servedShardFactor       int64   = 16
	servedMeshDegreeTarget  int64   = 6
	servedMeshDegreeMin     int64   = 4
	servedMeshDegreeMax     int64   = 8
	servedForwardThreshold  float32 = 0.75
	servedShardMultiplier   float32 = 2.5
	servedRandomMessageSize int64   = 50_000
)

// newServedNode builds a node whose dynamic config is already serving the
// values above, which is the state a running gateway is in by the time it wires
// its mesh.
func newServedNode(t *testing.T, clusterID string) mump2p.Engine {
	t.Helper()

	cnt := test_utils.GetClean(t)
	cfg := test_utils.NewTestConfig(cnt.Ctx, cnt.Log, clusterID, commontest.GetFreePortT(t), nil)
	cfg.Rotator = commonconfig.NewConfigRotator(
		cnt.Ctx,
		cnt.Log,
		&commonentities.OptimumConfig{
			RandomMessageSize:        servedRandomMessageSize,
			ShardFactor:              servedShardFactor,
			PublisherShardMultiplier: servedShardMultiplier,
			ForwardShardThreshold:    servedForwardThreshold,
			MeshDegreeTarget:         servedMeshDegreeTarget,
			MeshDegreeMin:            servedMeshDegreeMin,
			MeshDegreeMax:            servedMeshDegreeMax,
		},
		"hoodi",
		clusterID,
		func(*commonentities.DynamicConfig) {},
	)

	return test_utils.NewTestNodeWithCfg(cnt.Ctx, t, cnt.Log, t.TempDir(), cfg)
}

// TestEffectiveRLNCParamsMatchTheServedConfig is the divergence guard between
// what an operator served and what the node's router actually forwards on.
//
// The startup log once reported the generation size the dynamic config was
// seeded with rather than the one the node resolved, which is how a node
// running at k=16 read as a node running at k=4. These are the numbers an
// operator surface may report, so they are pinned to the served config and not
// to whatever the built-in defaults happen to be.
func TestEffectiveRLNCParamsMatchTheServedConfig(t *testing.T) {
	node := newServedNode(t, "optimum_rlnc_params")

	got, ok := node.EffectiveRLNCParams()
	require.True(t, ok, "a started node must be able to report what it resolved")

	require.Equal(t, uint32(servedShardFactor), got.ShardFactor)
	require.Equal(t, int(servedMeshDegreeTarget), got.MeshDegreeTarget)
	require.Equal(t, int(servedMeshDegreeMin), got.MeshDegreeMin)
	require.Equal(t, int(servedMeshDegreeMax), got.MeshDegreeMax)
	require.InDelta(t, float64(servedShardMultiplier), got.RedundancyFraction, 1e-6)
	require.InDelta(t, float64(servedForwardThreshold), got.ForwardThreshold, 1e-6)

	// int(16 * 0.75): the rank a node must strictly exceed before it recodes and
	// forwards. At the built-in k=4 this gate would open at 3 instead of 12.
	require.Equal(t, 12, got.ForwardRankThreshold)
	require.Positive(t, got.MaxShardSize)
}
