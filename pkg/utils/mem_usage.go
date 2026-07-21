package utils

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/getoptimum/optimum-common/pkg/logger"
)

func DumpMemStat(ctx context.Context, log logger.AppLogger, reportInterval time.Duration) {
	runtime.GC()
	start := time.Now()
	ticker := time.NewTicker(reportInterval)
	defer ticker.Stop()

	var prev runtime.MemStats
	runtime.ReadMemStats(&prev)
	prev = *MeasureMemStats(log, start, &prev)

	for {
		select {
		case <-ctx.Done():
			MeasureMemStats(log, start, &prev)
			return
		case <-ticker.C:
			prev = *MeasureMemStats(log, start, &prev)
		}
	}
}

func MeasureMemStats(log logger.AppLogger, startAt time.Time, prev *runtime.MemStats) *runtime.MemStats {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	log.Info(fmt.Sprintf(
		"elapsed=%s heap_alloc=%s (+%s) heap_inuse=%s heap_objects=%d (+%d) num_gc=%d (+%d) goroutines=%d",
		time.Since(startAt).Truncate(time.Second),
		FormatBytes(ms.HeapAlloc),
		FormatDelta(ms.HeapAlloc, prev.HeapAlloc),
		FormatBytes(ms.HeapInuse),
		ms.HeapObjects,
		int64(ms.HeapObjects)-int64(prev.HeapObjects), //nolint:gosec // ok
		ms.NumGC,
		int64(ms.NumGC)-int64(prev.NumGC),
		runtime.NumGoroutine(),
	))
	return &ms
}

func FormatBytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}

func FormatDelta(cur, prev uint64) string {
	if cur >= prev {
		return "+" + FormatBytes(cur-prev)
	}
	return "-" + FormatBytes(prev-cur)
}
