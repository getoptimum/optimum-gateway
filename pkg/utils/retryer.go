package utils

import (
	"context"
	"net/http"
	"slices"
	"time"

	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	commonrand "github.com/getoptimum/optimum-common/pkg/rand"
)

const retryAttempts = 3

func RetryPostRequest[T any](
	ctx context.Context,
	url string,
	payload any,
	headers map[string]string,
	codes ...int,
) (res *T, code int, err error) {
	return retryRequest(ctx, func() (*T, int, error) {
		return commonnet.PostCurl[T](ctx, url, payload, headers)
	}, func(i int) bool {
		if IsPostSuccess(i) {
			return true
		}
		if len(codes) == 0 {
			return false
		}
		return slices.Contains(codes, i)
	})
}

func RetryGetRequest[T any](ctx context.Context, url string, headers map[string]string) (res *T, code int, err error) {
	return retryRequest(ctx, func() (*T, int, error) {
		return commonnet.GetCurl[T](ctx, url, headers)
	}, func(code int) bool { return code == http.StatusOK })
}

func retryRequest[T any](ctx context.Context, fn func() (*T, int, error), ok func(int) bool) (res *T, code int, err error) {
	for attempt := range retryAttempts {
		res, code, err = fn()
		if ok(code) {
			return res, code, err
		}
		if attempt == retryAttempts-1 {
			break
		}
		delay := min(time.Second<<attempt, 8*time.Second)
		delay += retryJitter(delay / 2)
		select {
		case <-ctx.Done():
			return res, code, err
		case <-time.After(delay):
		}
	}
	return res, code, err
}

func retryJitter(maxJitter time.Duration) time.Duration {
	if maxJitter <= 0 {
		return 0
	}
	jitter, err := commonrand.RandBetween(0, int(maxJitter))
	if err != nil {
		return 0
	}
	return time.Duration(jitter)
}

// IsPostSuccess returns true for POST success codes used by bootstrap endpoints.
func IsPostSuccess(code int) bool {
	return code == http.StatusOK || code == http.StatusCreated
}
