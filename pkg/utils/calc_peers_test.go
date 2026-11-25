package utils_test

import (
	"fmt"
	"testing"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"

	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

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
