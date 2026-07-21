package bootstrapper

import (
	"fmt"
	"time"

	commonrand "github.com/getoptimum/optimum-common/pkg/rand"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// heartbeatBootstrapServer periodically sends a heartbeat to the bootstrap server to keep our registration active
// so other gateways can discover us and connect to us as a bootnode
func (s *Service) heartbeatBootstrapServer(bootURL string, payload *entities.GatewayInfo) {
	// ttl for registration is 1 hour. so we periodically send a heartbeat
	// to prevent all gateways from sending a heartbeat at the same time, we add some random jitter to the heartbeat interval
	timer := time.NewTimer(GenerateRandomDelay(5*time.Minute, 30*time.Minute))
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			if _, code, err := utils.RetryPostRequest[any](s.ctx, bootURL, payload, s.bearerAuthHeader(s.ctx)); !utils.IsPostSuccess(code) {
				s.log.Error("failed to send heartbeat to bootstrap server", fmt.Errorf("status code: %d, error: %w", code, err))
			} else {
				s.log.Info("sent heartbeat to bootstrap server")
			}
			timer.Reset(GenerateRandomDelay(5*time.Minute, 30*time.Minute))
		}
	}
}

func GenerateRandomDelay(minDelay, maxDelay time.Duration) time.Duration {
	sec, err := commonrand.RandBetween(int(minDelay.Seconds()), int(maxDelay.Seconds()))
	if err != nil || sec <= 0 {
		return minDelay
	}
	return time.Duration(sec) * time.Second
}
