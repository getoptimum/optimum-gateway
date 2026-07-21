package utils_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestBootstrapRegisterURL(t *testing.T) {
	tests := map[string]struct {
		host                string
		wantRegisterURL     string
		wantBlockLatencyURL string
	}{
		"hostname": {
			host:                "bootstrap.example.com",
			wantRegisterURL:     "https://bootstrap.example.com/api/v2/gateways/register",
			wantBlockLatencyURL: "https://bootstrap.example.com/api/v2/handle_block_latency",
		},
		"base url without trailing slash": {
			host:                "https://bootstrap.example.com",
			wantRegisterURL:     "https://bootstrap.example.com/api/v2/gateways/register",
			wantBlockLatencyURL: "https://bootstrap.example.com/api/v2/handle_block_latency",
		},
		"base url with trailing slash": {
			host:                "https://bootstrap.example.com/",
			wantRegisterURL:     "https://bootstrap.example.com/api/v2/gateways/register",
			wantBlockLatencyURL: "https://bootstrap.example.com/api/v2/handle_block_latency",
		},
		"base url with path preserved": {
			host:                "https://bootstrap.example.com/root",
			wantRegisterURL:     "https://bootstrap.example.com/root/api/v2/gateways/register",
			wantBlockLatencyURL: "https://bootstrap.example.com/root/api/v2/handle_block_latency",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.wantRegisterURL, utils.BootstrapRegisterURL(tt.host))
			require.Equal(t, tt.wantBlockLatencyURL, utils.BootstrapHandleBlockLatencyURL(tt.host))
		})
	}
}

func TestBootstrapExposeNodesURL(t *testing.T) {
	tests := map[string]struct {
		host         string
		clusterID    string
		version      string
		exposeAmount string
		want         string
	}{
		"hostname": {
			host:         "bootstrap.example.com",
			clusterID:    "cluster-a",
			version:      "v1.0.0",
			exposeAmount: "10",
			want:         "https://bootstrap.example.com/api/v2/expose-nodes?cluster_id=cluster-a&expose_amount=10&version=v1.0.0",
		},
		"base url with trailing slash": {
			host:         "https://bootstrap.example.com/",
			clusterID:    "cluster-a",
			version:      "v1.0.0",
			exposeAmount: "10",
			want:         "https://bootstrap.example.com/api/v2/expose-nodes?cluster_id=cluster-a&expose_amount=10&version=v1.0.0",
		},
		"values are escaped": {
			host:         "https://bootstrap.example.com",
			clusterID:    "cluster a",
			version:      "v1/2",
			exposeAmount: "10+1",
			want:         "https://bootstrap.example.com/api/v2/expose-nodes?cluster_id=cluster+a&expose_amount=10%2B1&version=v1%2F2",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, utils.BootstrapExposeNodesURL(tt.host, tt.clusterID, tt.version, tt.exposeAmount))
		})
	}
}

func TestBootstrapForkDigestURL(t *testing.T) {
	tests := map[string]struct {
		host    string
		chainID chain.Chain
		want    string
	}{
		"hostname": {
			host:    "bootstrap.example.com",
			chainID: "hoodi",
			want:    "https://bootstrap.example.com/api/v2/fork-digest?chain_id=hoodi",
		},
		"base url with trailing slash": {
			host:    "https://bootstrap.example.com/",
			chainID: chain.ChainHoodi,
			want:    "https://bootstrap.example.com/api/v2/fork-digest?chain_id=hoodi",
		},
		"chain id escaped": {
			host:    "https://bootstrap.example.com",
			chainID: "hoodi/mainnet",
			want:    "https://bootstrap.example.com/api/v2/fork-digest?chain_id=hoodi%2Fmainnet",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tt.want, utils.BootstrapForkDigestURL(tt.host, tt.chainID))
		})
	}
}

func TestForkDigestHubForksRawURL(t *testing.T) {
	require.Equal(t,
		"https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/hoodi/forks.json",
		utils.ForkDigestHubForksRawURL(chain.ChainHoodi))
	require.Equal(t,
		"https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/mainnet/forks.json",
		utils.ForkDigestHubForksRawURL(chain.ChainMainnet))
}

func TestForkDigestHubPeersRawURL(t *testing.T) {
	require.Equal(t,
		"https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/hoodi/optimum_ethereum_hoodi_v0_1_peers.json",
		utils.ForkDigestHubPeersRawURL(chain.ChainHoodi, "optimum_ethereum_hoodi_v0_1"))
	require.Equal(t,
		"https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/mainnet/optimum_ethereum_mainnet_v0_1_peers.json",
		utils.ForkDigestHubPeersRawURL(chain.ChainMainnet, "optimum_ethereum_mainnet_v0_1"))
}
