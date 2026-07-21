package forks

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

var (
	// SyncInterval how often renew forks map. Refresh it per epoch by default
	SyncInterval = 13 * time.Minute

	curlTimeout          = 30 * time.Second
	initialLoadAttempts  = 12
	initialLoadRetryWait = 5 * time.Second
)

func (s *Service) loadInitialForkDigest(ctx context.Context) error {
	var lastErr error
	for attempt := range initialLoadAttempts {
		err := s.loadForkDigest(ctx)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == initialLoadAttempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(initialLoadRetryWait):
		}
	}
	return fmt.Errorf("bootstrap and forkdigest-hub unavailable: %w", lastErr)
}

func (s *Service) loadForkDigest(ctx context.Context) error {
	errB := s.loadFromBootstrap(ctx)
	if errB == nil {
		return nil
	}
	errG := s.loadFromGitHub(ctx)
	if errG == nil {
		s.log.Info("fork digest loaded from forkdigest-hub after bootstrap failed",
			logger.WithString("bootstrap_error", errB.Error()),
		)
		return nil
	}
	return fmt.Errorf("bootstrap: %w; forkdigest-hub: %w", errB, errG)
}

func (s *Service) bgRefreshForkDigest(ctx context.Context) {
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if errB := s.loadFromBootstrap(ctx); errB != nil {
				s.log.Error("periodic fork digest fetch from bootstrap failed", errB)
				if errG := s.loadFromGitHub(ctx); errG != nil {
					s.log.Error("periodic fork digest fetch from forkdigest-hub failed", errG)
				}
			}
		}
	}
}

func (s *Service) bearerAuthHeader(ctx context.Context) map[string]string {
	tok, err := s.authMgr.ServicesToken(ctx)
	if err != nil || tok == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + tok}
}

func (s *Service) loadFromBootstrap(c context.Context) error {
	ctx, cancel := context.WithTimeout(c, curlTimeout)
	defer cancel()
	type forkDigestResponse struct {
		ChainID    string `json:"chain_id"`
		ForkDigest string `json:"fork_digest"`
		FutureFork string `json:"future_fork"`
	}
	url := utils.BootstrapForkDigestURL(s.cfg.RemoteBootstrapURL, s.appChain)
	res, code, err := commonnet.GetCurl[forkDigestResponse](ctx, url, s.bearerAuthHeader(ctx))
	if err != nil || code != http.StatusOK {
		return fmt.Errorf("bootstrap fork digest request failed: code=%d err=%w", code, err)
	}
	if res == nil || strings.TrimSpace(res.ForkDigest) == "" {
		return fmt.Errorf("empty fork digest in bootstrap response")
	}
	s.applyForkDigest(res.ForkDigest, res.FutureFork)
	s.log.Info("fork digest updated",
		logger.WithString("source", "bootstrap"),
		logger.WithString("fork_digest", res.ForkDigest),
		logger.WithString("future_fork", res.FutureFork),
	)
	return nil
}

func (s *Service) loadFromGitHub(c context.Context) error {
	ctx, cancel := context.WithTimeout(c, curlTimeout)
	defer cancel()
	type hubForksFile struct {
		Forks      []string `json:"forks"`
		FutureFork string   `json:"future_fork"`
	}

	url := utils.ForkDigestHubForksRawURL(s.appChain)
	res, code, err := commonnet.GetCurl[hubForksFile](ctx, url, nil)
	if err != nil {
		return fmt.Errorf("forkdigest-hub: %w", err)
	}
	if code != http.StatusOK || res == nil || len(res.Forks) == 0 {
		return fmt.Errorf("forkdigest-hub: code=%d empty or missing", code)
	}
	digest := strings.ToLower(strings.TrimSpace(res.Forks[len(res.Forks)-1]))
	if len(digest) != 8 {
		return fmt.Errorf("forkdigest-hub: invalid digest length %d", len(digest))
	}
	s.applyForkDigest(digest, res.FutureFork)
	s.log.Info("fork digest updated",
		logger.WithString("source", "forkdigest-hub"),
		logger.WithString("fork_digest", digest),
		logger.WithString("future_fork", res.FutureFork),
	)
	return nil
}
