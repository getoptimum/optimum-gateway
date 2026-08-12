package utils_test

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestCalculateMaxSize(t *testing.T) {
	t.Run("rejects non-positive input", func(t *testing.T) {
		_, err := utils.CalculateMaxSize(0)
		require.ErrorContains(t, err, "src must be positive")
	})

	t.Run("adds twenty percent overhead", func(t *testing.T) {
		size, err := utils.CalculateMaxSize(100)
		require.NoError(t, err)
		require.Equal(t, 120, size)
	})

	t.Run("accepts max allowed value without overflow", func(t *testing.T) {
		maxAllowed := int64(math.MaxInt)
		src := maxAllowed - maxAllowed/5
		size, err := utils.CalculateMaxSize(src)
		require.NoError(t, err)
		require.Greater(t, size, 0)
	})

	t.Run("rejects overflow after overhead", func(t *testing.T) {
		_, err := utils.CalculateMaxSize(int64(math.MaxInt))
		require.ErrorContains(t, err, "src is too large")
	})
}

func TestPeersFromStrings(t *testing.T) {
	host, err := libp2p.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, host.Close())
	})

	addrInfo := peer.AddrInfo{
		ID:    host.ID(),
		Addrs: host.Addrs(),
	}
	jsonEncoded := commonnet.AddressInfoToString(addrInfo)
	rawP2PAddr := fmt.Sprintf("%s/p2p/%s", addrInfo.Addrs[0].String(), addrInfo.ID.String())

	peers, err := utils.PeersFromStrings([]string{jsonEncoded, rawP2PAddr})
	require.NoError(t, err)
	require.Len(t, peers, 2)
	require.Equal(t, addrInfo.ID, peers[0].ID)
	require.Equal(t, addrInfo.ID, peers[1].ID)
	require.NotEmpty(t, peers[0].Addrs)
	require.NotEmpty(t, peers[1].Addrs)

	_, err = utils.PeersFromStrings([]string{"not-a-peer"})
	require.ErrorContains(t, err, "error parsing peer")
}

func TestBootstrapURLFallbacks(t *testing.T) {
	require.Equal(
		t,
		"https://%zz/api/v2/expose-nodes",
		utils.BootstrapExposeNodesURL("https://%zz", "cluster-a", "v1.0.0", "10"),
	)
	require.Equal(
		t,
		"https://%zz/api/v2/fork-digest?chain_id=hoodi%2Fmainnet",
		utils.BootstrapForkDigestURL("https://%zz", "hoodi/mainnet"),
	)
}

func TestRetryGetRequestStopsWhenContextIsCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		cancel()
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"value":"retry-me"}`))
	}))
	t.Cleanup(srv.Close)

	got, code, err := utils.RetryGetRequest[retryResponse](ctx, srv.URL, nil)

	require.ErrorContains(t, err, "context canceled")
	require.Zero(t, code)
	require.Nil(t, got)
	require.LessOrEqual(t, calls.Load(), int32(1))
}
