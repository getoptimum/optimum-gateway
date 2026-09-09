package stream

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/service/streamhub"
)

// TestWithDefaultsDoesNotMutateCaller pins the copy-on-entry: without it, a
// default applied through the pointer would mutate caller-owned state.
func TestWithDefaultsDoesNotMutateCaller(t *testing.T) {
	in := Config{}
	got := withDefaults(&in)

	require.Equal(t, defaultMaxConns, got.MaxConns)
	require.Equal(t, defaultMaxConnsPerSub, got.MaxConnsPerSub)
	require.Equal(t, streamhub.DefaultBufferSize, got.BufferSize)
	require.NotNil(t, got.Limiter, "a nil Limiter is filled in on the copy")

	require.Equal(t, Config{}, in, "the caller's Config must come back untouched")
}

// oldestStartOf reads the limiter's answer under its own lock, which is how
// publishOldest reads it.
func oldestStartOf(l *ConnLimiter) time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.oldestStart()
}

// capturePublished swaps the telemetry seam so a test sees what the limiter
// actually published, not just what it computed.
func capturePublished(t *testing.T) *[]float64 {
	t.Helper()
	prev := publishOldestStart
	var got []float64
	publishOldestStart = func(v float64) { got = append(got, v) }
	t.Cleanup(func() { publishOldestStart = prev })
	return &got
}

// TestConnLimiterPublishesOldestStart pins the wiring, not the computation:
// dropping either publishOldest call would leave the value correct in memory
// while the gauge operators read went stale.
func TestConnLimiterPublishesOldestStart(t *testing.T) {
	published := capturePublished(t)
	l := NewConnLimiter(10, 10)

	id1, ok := l.acquire("sub-a")
	require.True(t, ok)
	require.Len(t, *published, 1, "acquire must publish")

	id2, ok := l.acquire("sub-b")
	require.True(t, ok)
	require.Len(t, *published, 2, "every membership change must publish")
	first := (*published)[0]
	require.Equal(t, first, (*published)[1], "a newer connection does not change the oldest")

	l.release("sub-b", id2)
	require.Len(t, *published, 3, "release must publish")
	require.Equal(t, first, (*published)[2], "releasing the newer one leaves the oldest published")

	l.release("sub-a", id1)
	require.Len(t, *published, 4)
	require.Zero(t, (*published)[3], "an empty limiter publishes 0, not a stale start")
}

// TestConnLimiterOldestStart covers what the published gauge is for: it must
// track the *oldest* live connection, which means a release has to be matched
// to the connection that actually ended. Start times are set explicitly rather
// than taken from the clock, so the ordering is not at the mercy of timer
// resolution.
func TestConnLimiterOldestStart(t *testing.T) {
	newRig := func(t *testing.T) (*ConnLimiter, [3]uint64, time.Time) {
		t.Helper()
		l := NewConnLimiter(10, 10)
		var ids [3]uint64
		for i, subject := range []string{"sub-a", "sub-b", "sub-c"} {
			id, ok := l.acquire(subject)
			require.True(t, ok)
			ids[i] = id
		}
		base := time.Now().Truncate(time.Second)
		l.mu.Lock()
		l.starts[ids[0]] = base
		l.starts[ids[1]] = base.Add(time.Minute)
		l.starts[ids[2]] = base.Add(2 * time.Minute)
		l.mu.Unlock()
		return l, ids, base
	}

	t.Run("reports the earliest start", func(t *testing.T) {
		l, _, base := newRig(t)
		require.Equal(t, base, oldestStartOf(l))
	})

	t.Run("releasing a newer connection leaves the oldest", func(t *testing.T) {
		l, ids, base := newRig(t)
		l.release("sub-c", ids[2])
		require.Equal(t, base, oldestStartOf(l))
		l.release("sub-b", ids[1])
		require.Equal(t, base, oldestStartOf(l))
	})

	t.Run("releasing the oldest promotes the next", func(t *testing.T) {
		l, ids, base := newRig(t)
		l.release("sub-a", ids[0])
		require.Equal(t, base.Add(time.Minute), oldestStartOf(l))
	})

	t.Run("zero once the last connection closes", func(t *testing.T) {
		l, ids, _ := newRig(t)
		for i, subject := range []string{"sub-a", "sub-b", "sub-c"} {
			l.release(subject, ids[i])
		}
		require.True(t, oldestStartOf(l).IsZero(), "an empty limiter must publish 0, not a stale start")
	})

	t.Run("ids are not reused after release", func(t *testing.T) {
		l, ids, _ := newRig(t)
		l.release("sub-a", ids[0])
		next, ok := l.acquire("sub-a")
		require.True(t, ok)
		require.NotContains(t, ids, next, "a recycled id would make release ambiguous")
	})
}
