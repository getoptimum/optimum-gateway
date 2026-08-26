package message_router

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecideAccelerate(t *testing.T) {
	require.Equal(t, accelerateFailOpen, decideAccelerate(nil, 1))
	require.Equal(t, accelerateFailOpen, decideAccelerate(&accelerateWindow{}, 1))

	w := &accelerateWindow{
		toSlot: 120,
		slots:  map[uint64]struct{}{100: {}, 101: {}},
	}
	require.Equal(t, accelerateOnList, decideAccelerate(w, 100))
	require.Equal(t, accelerateOnList, decideAccelerate(w, 101))
	require.Equal(t, accelerateNotOnList, decideAccelerate(w, 110))
	require.Equal(t, accelerateFailOpen, decideAccelerate(w, 121))
}
