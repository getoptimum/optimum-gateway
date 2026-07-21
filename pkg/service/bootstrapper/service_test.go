package bootstrapper_test

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/identity"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/forks"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/service/bootstrapper"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func getTestSrv(t *testing.T) (*bootstrapper.Service, *test_utils.LocalBootstrapServer, *config.AppConfig) {
	t.Helper()

	cnt := test_utils.GetClean(t)
	rig := test_utils.NewAuthTestRig(t)
	bootstrap := test_utils.NewLocalBootstrapServerWithRig(t, rig)
	bootstrap.SetForkResponse(map[string]any{
		"chain_id":    "hoodi",
		"fork_digest": "deadbeef",
		"future_fork": "",
	})
	cfg := rig.AppCfg(t)
	cfg.RemoteBootstrapURL = bootstrap.URL()
	cfg.Version = "v0.0.1-test"
	srvAuth, err := auth_token.New(t.Context(), cnt.Log, cfg)
	require.NoError(t, err)
	_, err = srvAuth.Token(t.Context())
	require.NoError(t, err)
	srvFork, err := forks.NewService(t.Context(), cfg, cnt.Log, srvAuth)
	require.NoError(t, err)

	return bootstrapper.NewService(cnt.Ctx, cnt.Log, cfg, srvAuth, srvFork), bootstrap, cfg
}

func TestRegisterAndGetMumP2PPeers_Success(t *testing.T) {
	// given
	srv, bootstrap, cfg := getTestSrv(t)

	// when
	peers, err := srv.RegisterAndGetMumP2PPeers()
	require.NoError(t, err)

	// then
	require.Len(t, peers, 1)
	id, err := identity.ExtractIdentityFromDir(cfg.IdentityMumP2PDir)
	require.NoError(t, err)
	require.Contains(t, peers[0], id.ID.String())
	registerReq := bootstrap.WaitRegisterRequest(t, time.Second)
	require.Contains(t, registerReq.Authorization, "Bearer ")
	exposeReq := bootstrap.WaitExposeNodesRequest(t, time.Second)
	require.Contains(t, exposeReq.Authorization, "Bearer ")
	require.Empty(t, exposeReq.Query.Get("chain_id"))
	require.Equal(t, cfg.Version, exposeReq.Query.Get("version"))
	require.Equal(t, cfg.GatewayClusterID, exposeReq.Query.Get("cluster_id"))
	require.Equal(t, "7", exposeReq.Query.Get("expose_amount"))

	t.Run("skip_invalid_mump2p_address", func(t *testing.T) {
		ma1, maErr := commonnet.MultiAddressBuilder(net.ParseIP("127.0.0.1"), 4001)
		require.NoError(t, maErr)
		pa := peer.AddrInfo{ID: peer.ID("peerValid"), Addrs: []ma.Multiaddr{ma1[0]}}
		bootstrap.SetMessagesResponse(map[string]any{
			"gw-valid":   map[string]any{"gateway_id": "gw-valid", "mump2p_addr": commonnet.AddressInfoToString(pa)},
			"gw-invalid": map[string]any{"gateway_id": "gw-invalid", "mump2p_addr": "not-a-valid-multiaddr"},
		})

		// when
		peers, err = srv.RegisterAndGetMumP2PPeers()
		require.NoError(t, err)

		// then
		require.Len(t, peers, 2)
		for _, p := range peers {
			require.True(t, strings.Contains(p, id.ID.String()) || strings.Contains(p, peer.ID("peerValid").String()))
		}
	})
	t.Run("serve same peerID", func(t *testing.T) {
		ma1, maErr := commonnet.MultiAddressBuilder(net.ParseIP("127.0.0.1"), 4001)
		require.NoError(t, maErr)
		pa := peer.AddrInfo{ID: id.ID, Addrs: []ma.Multiaddr{ma1[0]}}
		bootstrap.SetMessagesResponse(map[string]any{
			"gw-test": map[string]any{
				"gateway_id":  "gw-test",
				"mump2p_addr": commonnet.AddressInfoToString(pa),
			},
		})

		// when
		peers, err = srv.RegisterAndGetMumP2PPeers()
		require.NoError(t, err)

		// then — no Fatal was called, peers returned normally
		require.NotEmpty(t, peers)
	})

	t.Run("skip_all_invalid_bootstrap_peers_except_self", func(t *testing.T) {
		bootstrap.SetMessagesResponse(map[string]any{
			"gw-invalid-1": map[string]any{"gateway_id": "gw-invalid-1", "mump2p_addr": "not-a-valid-multiaddr"},
			"gw-invalid-2": map[string]any{"gateway_id": "gw-invalid-2", "mump2p_addr": "still-not-a-valid-multiaddr"},
		})

		// when
		peers, err = srv.RegisterAndGetMumP2PPeers()
		require.NoError(t, err)

		// then
		require.Len(t, peers, 1)
		require.Contains(t, peers[0], id.ID.String())
	})
}
