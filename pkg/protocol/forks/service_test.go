package forks_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/forks"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	testutils "github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func newTestService(t *testing.T) (*forks.Service, *testutils.LocalBootstrapServer) {
	t.Helper()
	cnt := testutils.GetClean(t)
	rig := testutils.NewAuthTestRig(t)
	bootstrap := testutils.NewLocalBootstrapServerWithRig(t, rig)
	bootstrap.SetForkResponse(map[string]any{
		"chain_id":    "hoodi",
		"fork_digest": "deadbeef",
		"future_fork": "DDEEFF00",
	})
	cfg := rig.AppCfg(t)
	cfg.RemoteBootstrapURL = bootstrap.URL()
	srvAuth, err := auth_token.New(t.Context(), cnt.Log, cfg)
	require.NoError(t, err)
	_, err = srvAuth.Token(t.Context())
	require.NoError(t, err)

	forks.SyncInterval = 500 * time.Millisecond
	srv, err := forks.NewService(t.Context(), cfg, cnt.Log, srvAuth)
	require.NoError(t, err)
	require.Equal(t, "deadbeef", srv.ActiveDigest())
	srv.Start(t.Context())
	return srv, bootstrap
}

func TestServiceApplyForkDigestReplacesSupportedSet(t *testing.T) {
	// given
	svc, localBootstrap := newTestService(t)
	require.True(t, svc.CheckForkSupported("deadbeef"))
	require.True(t, svc.CheckForkSupported("ddeeff00"))

	valid := []string{
		"mump2p_aggregated_messages",
		"/eth2/deadbeef/beacon_block/ssz_snappy",
		"/eth2/ddeeff00/beacon_block/ssz_snappy",
	}
	for _, topic := range valid {
		require.Truef(t, svc.TopicSupported(topic), topic)
	}

	// when
	localBootstrap.SetForkResponse(map[string]any{
		"chain_id":    "hoodi",
		"fork_digest": " c6ecb76c ",
		"future_fork": "",
	})

	// then
	require.Eventually(t, func() bool {
		return svc.CheckForkSupported("c6ecb76c")
	}, 10*time.Second, 1*time.Second, "fork digest should be loaded from bootstrap")
	require.Equal(t, "c6ecb76c", svc.ActiveDigest())
	require.False(t, svc.TopicSupported("/eth2/ddeeff00/beacon_block/ssz_snappy"))
	require.True(t, svc.CheckForkSupported("c6ecb76c"))
	require.False(t, svc.CheckForkSupported("deadbeef"))
	require.False(t, svc.CheckForkSupported("ddeeff00"))

	t.Run("check supported forks", func(t *testing.T) {
		localBootstrap.SetForkResponse(map[string]any{
			"chain_id":    "hoodi",
			"fork_digest": " DDEEFF00 ",
			"future_fork": " c6ecb76c ",
		})
		require.Eventually(t, func() bool {
			return svc.CheckForkSupported("ddeeff00")
		}, 10*time.Second, 1*time.Second, "fork digest should be loaded from bootstrap")
		require.True(t, svc.CheckForkSupported("c6ecb76c"))
		require.True(t, svc.CheckForkSupported("ddeeff00"))
		require.True(t, svc.CheckForkSupported(" DDEEFF00 "))
		require.False(t, svc.CheckForkSupported("12345678"))
	})

	t.Run("check active digest", func(t *testing.T) {
		svc.SetObservedDigest(" c6ecb76c ")
		require.Equal(t, "c6ecb76c", svc.ActiveDigest())

		svc.SetObservedDigest(" DDEEFF00 ")
		require.Equal(t, "ddeeff00", svc.ActiveDigest())

		svc.SetObservedDigest("")
		require.Equal(t, "ddeeff00", svc.ActiveDigest())
	})
}
