package stream

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
)

// TestWithDefaultsDoesNotMutateCaller pins the reason withDefaults may take a
// pointer at all. The pointer exists only to stay under gocritic's hugeParam
// threshold; the copy-on-entry is what keeps it safe, and without this test a
// future default applied through the parameter would silently start mutating
// caller-owned state.
func TestWithDefaultsDoesNotMutateCaller(t *testing.T) {
	in := Config{}
	got := withDefaults(&in)

	require.Equal(t, defaultMaxConns, got.MaxConns)
	require.Equal(t, defaultMaxConnsPerSub, got.MaxConnsPerSub)
	require.Equal(t, streamhub.DefaultBufferSize, got.BufferSize)
	require.NotNil(t, got.Limiter, "a nil Limiter is filled in on the copy")

	require.Equal(t, Config{}, in, "the caller's Config must come back untouched")
}
