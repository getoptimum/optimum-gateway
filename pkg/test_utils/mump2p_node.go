package test_utils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/stretchr/testify/require"

	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/test_utils"
	cfgpkg "github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
)

func NewTestNode(
	ctx context.Context,
	t *testing.T,
	log logger.AppLogger,
	clusterID string,
	identityDir string,
	opts ...mump2p.NodeOption,
) mump2p.Engine {
	t.Helper()

	cfg := NewTestConfig(ctx, log, clusterID, test_utils.GetFreePortT(t), nil)
	return NewTestNodeWithCfg(ctx, t, log, identityDir, cfg, opts...)
}

func NewTestNodeWithCfg(
	ctx context.Context,
	t *testing.T,
	log logger.AppLogger,
	identityDir string,
	cfg *mump2p.Config,
	opts ...mump2p.NodeOption,
) mump2p.Engine {
	t.Helper()

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", test_utils.GetFreePortT(t))),
	)
	require.NoError(t, err)

	node, err := mump2p.NewNodeWithHost(
		ctx,
		log.With(logger.WithService("mump2p_test")),
		cfg,
		h,
		identityDir,
		append(TestNodeOptions(), opts...)...,
	)
	require.NoError(t, err)

	t.Cleanup(node.Stop)
	return node
}

// TestNodeOptions are the node options every test node needs, ahead of any the
// caller adds. The coder sidecar is not running under `go test`, so the node gets
// an in-process one; see PassthroughCoder for why it cannot be the real coder.
func TestNodeOptions() []mump2p.NodeOption {
	return []mump2p.NodeOption{mump2p.WithCoder(NewPassthroughCoder())}
}

func NewTestConfig(
	ctx context.Context,
	log logger.AppLogger,
	clusterID string,
	listenPort int,
	boostrapPeers []string,
) *mump2p.Config {
	cfg := &mump2p.Config{
		ClusterID:                clusterID,
		ListenPort:               listenPort,
		MaxMessageSize:           cfgpkg.DefaultMaxMessageSize,
		RandomMessageSize:        cfgpkg.DefaultRandomMessageSize,
		ShardFactor:              int(cfgpkg.DefaultShardFactor),
		PublisherShardMultiplier: cfgpkg.DefaultPublisherShardMultiplier,
		ForwardShardThreshold:    cfgpkg.DefaultForwardShardThreshold,
		MeshDegreeTarget:         int(cfgpkg.DefaultMeshDegreeTarget),
		MeshDegreeMin:            int(cfgpkg.DefaultMeshDegreeMin),
		MeshDegreeMax:            int(cfgpkg.DefaultMeshDegreeMax),
		Rotator: commonconfig.NewConfigRotator(
			ctx,
			log,
			&commonentities.OptimumConfig{
				MaxMessageSize:           cfgpkg.DefaultMaxMessageSize,
				RandomMessageSize:        cfgpkg.DefaultRandomMessageSize,
				ShardFactor:              cfgpkg.DefaultShardFactor,
				PublisherShardMultiplier: cfgpkg.DefaultPublisherShardMultiplier,
				ForwardShardThreshold:    cfgpkg.DefaultForwardShardThreshold,
				MeshDegreeTarget:         cfgpkg.DefaultMeshDegreeTarget,
				MeshDegreeMin:            cfgpkg.DefaultMeshDegreeMin,
				MeshDegreeMax:            cfgpkg.DefaultMeshDegreeMax,
			},
			"hoodi",
			clusterID,
			func(*commonentities.DynamicConfig) {},
		),
	}
	if len(boostrapPeers) > 0 {
		cfg.BootstrapPeers = boostrapPeers
	}
	return cfg
}

func ConnectNodes(ctx context.Context, t *testing.T, left, right mump2p.Engine) {
	t.Helper()

	require.NoError(t, left.GetHost().Connect(ctx, right.GetHostInfo()))
	require.Eventually(t, func() bool {
		return len(left.GetPeers()) == 1 && len(right.GetPeers()) == 1
	}, 10*time.Second, 100*time.Millisecond)
}
