package forks

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonsyncx "github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	testutils "github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

var defaultTransportMu sync.Mutex

func newUnitTestService() *Service {
	svc := &Service{
		cfg:            &config.AppConfig{RemoteBootstrapURL: "https://bootstrap.example.com"},
		log:            logger.NewAppSLogger(logger.Debug),
		supportedForks: commonsyncx.NewRWMap[string, struct{}](),
		topicForkCache: commonsyncx.NewRWMap[string, string](),
		appChain:       chain.ChainHoodi,
		appChainID:     chain.ChainHoodi.ID(),
	}
	svc.cfgForkDigest.Store("c6ecb76c")
	return svc
}

func rewriteGitHubRequestsTo(t *testing.T, serverURL string) {
	t.Helper()

	target, err := url.Parse(serverURL)
	require.NoError(t, err)

	defaultTransportMu.Lock()
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Scheme = target.Scheme
		clone.URL.Host = target.Host
		clone.Host = target.Host
		return prevTransport.RoundTrip(clone)
	})
	t.Cleanup(func() {
		http.DefaultTransport = prevTransport
		defaultTransportMu.Unlock()
	})
}

func newEnabledAuthManager(t *testing.T) *auth_token.Service {
	t.Helper()

	rig := testutils.NewAuthTestRig(t)
	mgr, err := auth_token.New(context.Background(), logger.NewAppSLogger(logger.Debug), rig.AppCfg(t))
	require.NoError(t, err)
	return mgr
}

func TestBearerAuthHeader(t *testing.T) {
	t.Run("disabled auth returns nil header", func(t *testing.T) {
		mgr, err := auth_token.New(context.Background(), logger.NewAppSLogger(logger.Debug), &config.AppConfig{EnableAuth: false})
		require.NoError(t, err)

		svc := newUnitTestService()
		svc.authMgr = mgr

		require.Nil(t, svc.bearerAuthHeader(context.Background()))
	})

	t.Run("enabled auth returns bearer token", func(t *testing.T) {
		svc := newUnitTestService()
		svc.authMgr = newEnabledAuthManager(t)

		token, err := svc.authMgr.ServicesToken(context.Background())
		require.NoError(t, err)
		require.NotEmpty(t, token)

		require.Equal(t, map[string]string{
			"Authorization": "Bearer " + token,
		}, svc.bearerAuthHeader(context.Background()))
	})
}

func TestLoadFromGitHub(t *testing.T) {
	t.Run("success stores latest digest and future fork", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "/getoptimum/forkdigest-hub/refs/heads/main/eth/hoodi/forks.json", r.URL.Path)
			_, _ = w.Write([]byte(`{"forks":["12345678"," ABCDEF12 "],"future_fork":" DDEEFF00 "}`))
		}))
		t.Cleanup(server.Close)
		rewriteGitHubRequestsTo(t, server.URL)

		svc := newUnitTestService()

		require.NoError(t, svc.loadFromGitHub(context.Background()))
		require.True(t, svc.CheckForkSupported("abcdef12"))
		require.True(t, svc.CheckForkSupported("ddeeff00"))
		require.False(t, svc.CheckForkSupported("12345678"))
		require.Equal(t, "abcdef12", svc.ActiveDigest())
	})

	t.Run("transport and decode errors are wrapped", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"forks":`))
		}))
		t.Cleanup(server.Close)
		rewriteGitHubRequestsTo(t, server.URL)

		svc := newUnitTestService()

		err := svc.loadFromGitHub(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "forkdigest-hub:")
	})

	t.Run("empty forks are rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"forks":[]}`))
		}))
		t.Cleanup(server.Close)
		rewriteGitHubRequestsTo(t, server.URL)

		svc := newUnitTestService()

		err := svc.loadFromGitHub(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "empty or missing")
	})

	t.Run("invalid digest length is rejected", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"forks":["123"]}`))
		}))
		t.Cleanup(server.Close)
		rewriteGitHubRequestsTo(t, server.URL)

		svc := newUnitTestService()

		err := svc.loadFromGitHub(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid digest length")
	})
}

func TestServiceAccessors(t *testing.T) {
	svc := newUnitTestService()
	require.Equal(t, chain.ChainHoodi, svc.AppChain())
	require.Equal(t, uint64(560048), svc.AppChainID())
}

func TestLoadFromGitHub_TransportFailure(t *testing.T) {
	defaultTransportMu.Lock()
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	t.Cleanup(func() {
		http.DefaultTransport = prevTransport
		defaultTransportMu.Unlock()
	})

	svc := newUnitTestService()

	err := svc.loadFromGitHub(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "forkdigest-hub:")
}
