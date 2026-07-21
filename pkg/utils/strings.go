package utils

import (
	"strconv"
	"strings"
)

func ShrinkPeerID(pid string) string {
	if len(pid) < 8 {
		return pid
	}
	return "..." + pid[len(pid)-8:]
}

// ParseCommaSeparatedUint64s parses a comma-separated list of base-10 uint64
// values. Whitespace around each segment and empty segments are tolerated
// ("42, , 1337" → [42, 1337]). Empty or whitespace-only input returns
// (nil, nil). Used by env-driven dev seeds like OPT_DEV_VALIDATOR_INDEXES.
func ParseCommaSeparatedUint64s(s string) ([]uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]uint64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
