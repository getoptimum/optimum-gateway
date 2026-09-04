package gossipsub_gateway

import (
	"net/http"
	"testing"
	"time"

	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
)

// clChecks are the checks derived from the CL host, which stream-only never starts.
var clChecks = []string{"cl_peers", "cl_health", "subscribed_topics"}

func newHealthTestService(streamOnly bool) *Service {
	return &Service{
		cfg:          &config.AppConfig{StreamOnly: streamOnly},
		libP2PTopics: syncx.NewRWMap[string, *libp2ppubsub.Topic](),
		startedAt:    time.Now(),
	}
}

func TestBuildHealthResponseStreamOnlySkipsCLChecks(t *testing.T) {
	resp, _ := newHealthTestService(true).BuildHealthResponse()

	for _, name := range clChecks {
		c, ok := resp.Checks[name]
		require.True(t, ok, "%s should stay visible", name)
		require.Equal(t, healthSkipped, c.Status)
		require.Nil(t, c.Value)
		require.NotContains(t, resp.Failing, name)
	}
	// Mesh checks alone decide the roll-up; no mump2p node here, so only they fail.
	require.ElementsMatch(t, []string{"mump2p_peers", "mump2p_health"}, resp.Failing)
	require.Equal(t, healthStatusDeg, resp.Status)
}

func TestBuildHealthResponseKeepsCLChecksWithoutStreamOnly(t *testing.T) {
	resp, code := newHealthTestService(false).BuildHealthResponse()

	for _, name := range clChecks {
		require.Equal(t, healthFail, resp.Checks[name].Status)
		require.Contains(t, resp.Failing, name)
	}
	require.Equal(t, http.StatusServiceUnavailable, code)
}
