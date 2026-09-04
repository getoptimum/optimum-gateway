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

// Only checks derived from Service state are asserted: cl_health and mump2p_health read
// process-global telemetry atomics that other tests in this binary can flip.
func TestBuildHealthResponse(t *testing.T) {
	t.Run("stream-only skips the CL checks", func(t *testing.T) {
		resp, _ := newHealthTestService(true).BuildHealthResponse()

		for _, name := range []string{"cl_peers", "cl_health", "subscribed_topics"} {
			c, ok := resp.Checks[name]
			require.True(t, ok, "%s should stay visible", name)
			require.Equal(t, healthSkipped, c.Status)
			require.Nil(t, c.Value)
			require.NotContains(t, resp.Failing, name)
		}
		// Mesh checks alone decide the roll-up; no mump2p node here, so it stays degraded.
		require.Contains(t, resp.Failing, "mump2p_peers")
		require.Equal(t, healthStatusDeg, resp.Status)
	})

	t.Run("keeps the CL checks otherwise", func(t *testing.T) {
		resp, code := newHealthTestService(false).BuildHealthResponse()

		for _, name := range []string{"cl_peers", "subscribed_topics"} {
			c := resp.Checks[name]
			require.Equal(t, healthFail, c.Status)
			require.NotNil(t, c.Value)
			require.Contains(t, resp.Failing, name)
		}
		require.Equal(t, http.StatusServiceUnavailable, code)
	})
}

func newHealthTestService(streamOnly bool) *Service {
	return &Service{
		cfg:          &config.AppConfig{StreamOnly: streamOnly},
		libP2PTopics: syncx.NewRWMap[string, *libp2ppubsub.Topic](),
		startedAt:    time.Now(),
	}
}
