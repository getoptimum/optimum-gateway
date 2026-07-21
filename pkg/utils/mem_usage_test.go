package utils_test

import (
	"context"
	"io"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func discardLogger() logger.AppLogger {
	return logger.InitLogger([]io.Writer{io.Discard}, logger.Info)
}

func TestFormatBytes(t *testing.T) {
	tests := map[uint64]string{
		0:                  "0 B",
		1:                  "1 B",
		1023:               "1023 B",
		1024:               "1.00 KiB",
		1536:               "1.50 KiB",
		1024 * 1024:        "1.00 MiB",
		1024 * 1024 * 1024: "1.00 GiB",
		1536 * 1024 * 1024: "1.50 GiB",
	}
	for input, want := range tests {
		require.Equal(t, want, utils.FormatBytes(input))
	}
}

func TestFormatDelta(t *testing.T) {
	tests := map[string]struct {
		cur, prev uint64
		want      string
	}{
		"cur greater than prev": {cur: 2048, prev: 1024, want: "+1.00 KiB"},
		"cur equal to prev":     {cur: 1024, prev: 1024, want: "+0 B"},
		"cur less than prev":    {cur: 0, prev: 1024, want: "-1.00 KiB"},
		"large decrease":        {cur: 0, prev: 1024 * 1024, want: "-1.00 MiB"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, utils.FormatDelta(tc.cur, tc.prev))
		})
	}
}

func TestMeasureMemStats_ReturnsNonNilStats(t *testing.T) {
	log := discardLogger()
	var prev runtime.MemStats
	runtime.ReadMemStats(&prev)

	got := utils.MeasureMemStats(log, time.Now(), &prev)

	require.NotNil(t, got)
	// Sanity: HeapSys should be positive on any running Go program.
	require.Greater(t, got.HeapSys, uint64(0))
}

func TestDumpMemStat_TerminatesOnContextCancel(t *testing.T) {
	log := discardLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling so the loop exits at first select

	done := make(chan struct{})
	go func() {
		// Use a long ticker interval; the canceled context causes immediate exit
		// through the ctx.Done() branch before the ticker ever fires.
		utils.DumpMemStat(ctx, log, time.Hour)
		close(done)
	}()

	select {
	case <-done:
		// expected — function returned promptly
	case <-time.After(5 * time.Second):
		t.Fatal("DumpMemStat did not return after context cancellation")
	}
}

func TestDumpMemStat_HandlesTickerBeforeCancel(t *testing.T) {
	log := discardLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		utils.DumpMemStat(ctx, log, 5*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DumpMemStat did not return after ticker-driven run")
	}
}
