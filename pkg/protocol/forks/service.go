// Package forks is the single source of truth for fork digest state.
// Supported digests load from bootstrap (forkdigest-hub fallback); ActiveDigest
// prefers the CL-observed digest so we track when connected CL peers switch forks.
package forks

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/getoptimum/optimum-common/pkg/chain"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonsyncx "github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/topics"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
)

// Service owns fork digest state: supported set, observed digest, topic caches, and refresh lifecycle.
type Service struct {
	cfg     *config.AppConfig
	log     logger.AppLogger
	authMgr *auth_token.Service

	supportedForks   *commonsyncx.RWMap[string, struct{}]
	topicForkCache   *commonsyncx.RWMap[string, string] // topic -> fork digest
	observedDigestLC atomic.Value                       // lowercased observed digest from CL peers
	cfgForkDigest    atomic.Value                       // active digest from bootstrap or forkdigest-hub
	cfgFutureDigest  atomic.Value                       // next digest from bootstrap or forkdigest-hub

	appChain   chain.Chain
	appChainID uint64
}

func NewService(ctx context.Context, appCfg *config.AppConfig, log logger.AppLogger, authMgr *auth_token.Service) (*Service, error) {
	// Auth off has no JWT chain_id. The dev-mode chain on AppConfig is the
	// same value InitRuntime already accepted.
	appChain := authMgr.Chain()
	if appChain == "" {
		parsed, parseErr := chain.ChainFromString(appCfg.Chain)
		if parseErr != nil {
			return nil, fmt.Errorf("dev chain: %w", parseErr)
		}
		appChain = parsed
	}
	srv := &Service{
		cfg:            appCfg,
		log:            log.With(logger.WithService("forks")),
		authMgr:        authMgr,
		supportedForks: commonsyncx.NewRWMap[string, struct{}](),
		topicForkCache: commonsyncx.NewRWMap[string, string](),
		appChain:       appChain,
		appChainID:     appChain.ID(),
	}

	if err := chain_state.LoadGenesisState(log, srv.appChain); err != nil {
		return nil, fmt.Errorf("failed to load genesis state: %w", err)
	}
	if err := srv.loadInitialForkDigest(ctx); err != nil {
		return nil, fmt.Errorf("failed to load fork digest: %w", err)
	}
	return srv, nil
}

// Start periodically refreshes fork digest from bootstrap (with hub fallback).
func (s *Service) Start(ctx context.Context) {
	go s.bgRefreshForkDigest(ctx)
}

func (s *Service) applyForkDigest(current, future string) {
	newMap := make(map[string]struct{}, 2)
	currentDigest := sanitizeForkDigest(current)
	if currentDigest != "" {
		newMap[currentDigest] = struct{}{}
		s.cfgForkDigest.Store(currentDigest)
	}
	futureDigest := sanitizeForkDigest(future)
	s.cfgFutureDigest.Store(futureDigest)
	if futureDigest != "" {
		newMap[futureDigest] = struct{}{}
	}
	s.supportedForks.Replace(newMap)
}

func (s *Service) CheckForkSupported(digest string) bool {
	_, ok := s.supportedForks.Load(sanitizeForkDigest(digest))
	return ok
}

// CanObserveMessageDigest reports whether a message on digest could advance the
// active fork. Callers can use it to avoid validating ordinary gossip twice.
func (s *Service) CanObserveMessageDigest(digest string) bool {
	digest = sanitizeForkDigest(digest)
	future, _ := s.cfgFutureDigest.Load().(string)
	return digest != "" && future != "" && digest == future &&
		s.CheckForkSupported(digest) && digest != s.ActiveDigest()
}

// ObserveMessageDigest promotes the active digest only to the configured future
// fork. Messages on the previous fork cannot roll an observed transition back.
func (s *Service) ObserveMessageDigest(digest string) bool {
	digest = sanitizeForkDigest(digest)
	if !s.CanObserveMessageDigest(digest) {
		return false
	}
	s.observedDigestLC.Store(digest)
	return true
}

func (s *Service) AppChainID() uint64 {
	return s.appChainID
}

func (s *Service) AppChain() chain.Chain {
	return s.appChain
}

// ActiveDigest prefers the CL-observed digest when set; otherwise the bootstrap/hub digest.
func (s *Service) ActiveDigest() string {
	if v := s.observedDigestLC.Load(); v != nil {
		if digest, _ := v.(string); digest != "" && s.CheckForkSupported(digest) {
			return digest
		}
	}
	if v := s.cfgForkDigest.Load(); v != nil {
		if digest, _ := v.(string); digest != "" {
			return digest
		}
	}
	return ""
}

// TopicSupported checks if a topic's fork digest is supported
func (s *Service) TopicSupported(topic string) bool {
	if topic == "mump2p_aggregated_messages" { // gateway-specific topic bypass remains
		return true
	}
	if val, ok := s.topicForkCache.Load(topic); ok {
		return s.CheckForkSupported(val)
	}
	fork := topics.GetForkDigestFromTopic(topic) // via helper in loader.go to avoid import cycle concerns
	s.topicForkCache.Store(topic, fork)
	return s.CheckForkSupported(fork)
}

func sanitizeForkDigest(digest string) string {
	return strings.ToLower(strings.TrimSpace(digest))
}
