package utils

func DiffUint64(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
