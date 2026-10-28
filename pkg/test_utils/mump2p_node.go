package test_utils

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/stretchr/testify/require"

	commonconfig "github.com/getoptimum/optimum-common/pkg/config"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/test_utils"
	cfgpkg "github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p"
)

func NewTestNode(
	ctx context.Context,
	t *testing.T,
	log logger.AppLogger,
	clusterID string,
	identityDir string,
	opts ...mum_p2p.NodeOption,
) *mum_p2p.Node {
	t.Helper()

	cfg := NewTestConfig(ctx, log, clusterID, test_utils.GetFreePortT(t), nil)
	return NewTestNodeWithCfg(ctx, t, log, identityDir, cfg, opts...)
}

func NewTestNodeWithCfg(
	ctx context.Context,
	t *testing.T,
	log logger.AppLogger,
	identityDir string,
	cfg *mum_p2p.Config,
	opts ...mum_p2p.NodeOption,
) *mum_p2p.Node {
	t.Helper()
	if !hasRLNCServerSemaphore(t, "/dev/shm") {
		t.Fatalf("rlnc server is not running, start it as `make run-rlnc-server`")
	}

	h, err := libp2p.New(
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", test_utils.GetFreePortT(t))),
	)
	require.NoError(t, err)

	node, err := mum_p2p.NewNodeWithHost(
		ctx,
		log,
		cfg,
		h,
		identityDir,
		opts...,
	)
	require.NoError(t, err)

	t.Cleanup(node.Stop)
	return node
}

func hasRLNCServerSemaphore(tb testing.TB, dir string) bool {
	tb.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "go_shm_rlnc_semaphore_mump2p-protocol_lane") {
			return true
		}
	}
	return false
}

func NewTestConfig(ctx context.Context, log logger.AppLogger, clusterID string, listenPort int, boostrapPeers []string) *mum_p2p.Config {
	cfg := &mum_p2p.Config{
		ClusterID:      clusterID,
		ListenPort:     listenPort,
		MaxMessageSize: cfgpkg.DefaultMaxMessageSize,
		Rotator: commonconfig.NewConfigRotator(
			ctx,
			log,
			&commonentities.OptimumConfig{
				MaxMessageSize:           cfgpkg.DefaultMaxMessageSize,
				RandomMessageSize:        cfgpkg.DefaultRandomMessageSize,
				ShardFactor:              cfgpkg.DefaultShardFactor,
				PublisherShardMultiplier: cfgpkg.DefaultPublisherShardMultiplier,
				ForwardShardThreshold:    cfgpkg.DefaultForwardShardThreshold,
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

func ConnectNodes(ctx context.Context, t *testing.T, left, right *mum_p2p.Node) {
	t.Helper()

	require.NoError(t, left.GetHost().Connect(ctx, right.GetHostInfo()))
	require.Eventually(t, func() bool {
		return len(left.GetPeers()) == 1 && len(right.GetPeers()) == 1
	}, 10*time.Second, 100*time.Millisecond)
}
