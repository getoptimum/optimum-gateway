package utils

import (
	"fmt"
	"math"
)

// CalculateMaxSize calculates the maximum size of a message that can be sent over the network.
// add some extra bytes for header and service info
// check possible overflow also
func CalculateMaxSize(src int64) (int, error) {
	if src <= 0 {
		return 0, fmt.Errorf("src must be positive: %d", src)
	}
	finalVal := src + int64(float64(src)*0.2)
	if finalVal < 0 || finalVal > math.MaxInt {
		return 0, fmt.Errorf("src is too large, final value not fit: %d", src)
	}
	return int(finalVal), nil
}
