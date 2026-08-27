package utils

import "context"

// RetryRequestForTest exposes the retry loop so a test can supply an fn that
// ignores ctx. That is the only way to observe the loop's own cancellation
// check: the real fn takes the same ctx, so once it is canceled the request
// fails before reaching the server and the attempt count stops moving.
func RetryRequestForTest[T any](
	ctx context.Context,
	fn func() (*T, int, error),
	ok func(int) bool,
) (res *T, code int, err error) {
	return retryRequest(ctx, fn, ok)
}
