package utils

import (
	"fmt"
	"math"
)

// CalculateMaxSize calculates the maximum size of a message that can be sent over the network.
// add some extra bytes for header and service info
// check possible overflow also using integer arithmetic
func CalculateMaxSize(src int64) (int, error) {
	if src <= 0 {
		return 0, fmt.Errorf("src must be positive: %d", src)
	}
	maxAllowed := int64(math.MaxInt)
	overhead := src / 5
	if src > maxAllowed-overhead {
		return 0, fmt.Errorf("src is too large, final value not fit: %d", src)
	}
	finalVal := src + overhead
	return int(finalVal), nil
}
