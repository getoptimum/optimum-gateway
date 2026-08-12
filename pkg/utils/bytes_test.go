package utils_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

func TestUnsafeCastToString(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		var b []byte = nil
		require.Equal(t, "", utils.UnsafeCastToString(b))
	})

	t.Run("empty non-nil slice", func(t *testing.T) {
		b := []byte{}
		require.Equal(t, "", utils.UnsafeCastToString(b))
	})

	t.Run("ordinary byte content", func(t *testing.T) {
		b := []byte("hello optimum gateway")
		require.Equal(t, "hello optimum gateway", utils.UnsafeCastToString(b))
	})
}
