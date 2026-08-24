package utils

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/getoptimum/optimum-common/pkg/chain"
)

const (
	BootstrapRegisterPath             = "/api/v2/gateways/register"
	BootstrapExposeNodesPath          = "/api/v2/expose-nodes"
	BootstrapHandleBlockLatencyV2     = "/api/v2/handle_block_latency"
	BootstrapHandleBlockLatencyBulkV2 = "/api/v2/handle_block_latency_bulk"
	BootstrapForkDigestPath           = "/api/v2/fork-digest"
)

// isBaseURL returns true if s looks like a full URL (http:// or https://).
func isBaseURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// BootstrapRegisterURL returns the full URL for gateway registration.
// host can be a hostname (uses https) or a full base URL.
func BootstrapRegisterURL(host string) string {
	if isBaseURL(host) {
		base := strings.TrimSuffix(host, "/")
		return base + BootstrapRegisterPath
	}
	u := url.URL{Scheme: "https", Host: host, Path: BootstrapRegisterPath}
	return u.String()
}

// BootstrapExposeNodesURL returns the full URL for peer discovery with query params.
// host can be a hostname (uses https) or a full base URL.
func BootstrapExposeNodesURL(host, clusterID, version, exposeAmount string) string {
	if isBaseURL(host) {
		parsed, err := url.Parse(strings.TrimSuffix(host, "/") + BootstrapExposeNodesPath)
		if err != nil {
			return host + BootstrapExposeNodesPath // fallback
		}
		q := parsed.Query()
		q.Set("cluster_id", clusterID)
		q.Set("version", version)
		q.Set("expose_amount", exposeAmount)
		parsed.RawQuery = q.Encode()
		return parsed.String()
	}
	q := url.Values{}
	q.Set("cluster_id", clusterID)
	q.Set("version", version)
	q.Set("expose_amount", exposeAmount)
	u := url.URL{
		Scheme:   "https",
		Host:     host,
		Path:     BootstrapExposeNodesPath,
		RawQuery: q.Encode(),
	}
	return u.String()
}

// BootstrapHandleBlockLatencyURL returns the full URL for v2 block latency endpoint.
func BootstrapHandleBlockLatencyURL(host string) string {
	return bootstrapPathURL(host, BootstrapHandleBlockLatencyV2)
}

// BootstrapHandleBlockLatencyBulkURL returns the full URL for v2 bulk block latency endpoint.
func BootstrapHandleBlockLatencyBulkURL(host string) string {
	return bootstrapPathURL(host, BootstrapHandleBlockLatencyBulkV2)
}

// BootstrapForkDigestURL returns the full URL for fork digest endpoint.
func BootstrapForkDigestURL(host string, chainID chain.Chain) string {
	base := bootstrapPathURL(host, BootstrapForkDigestPath)
	parsed, err := url.Parse(base)
	if err != nil {
		return base + "?chain_id=" + url.QueryEscape(chainID.String())
	}
	q := parsed.Query()
	q.Set("chain_id", chainID.String())
	parsed.RawQuery = q.Encode()
	return parsed.String()
}

// ForkDigestHubForksRawURL is forkdigest-hub raw forks.json for chain.
func ForkDigestHubForksRawURL(chainID chain.Chain) string {
	return fmt.Sprintf(
		"https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/%s/forks.json",
		chainID.String(),
	)
}

// ForkDigestHubPeersRawURL is forkdigest-hub raw peers file scoped to one cluster.
func ForkDigestHubPeersRawURL(chainID chain.Chain, clusterID string) string {
	return fmt.Sprintf(
		"https://raw.githubusercontent.com/getoptimum/forkdigest-hub/refs/heads/main/eth/%s/%s_peers.json",
		chainID.String(),
		clusterID,
	)
}

func bootstrapPathURL(host, path string) string {
	if isBaseURL(host) {
		return strings.TrimSuffix(host, "/") + path
	}
	u := url.URL{Scheme: "https", Host: host, Path: path}
	return u.String()
}
