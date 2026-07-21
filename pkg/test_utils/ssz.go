package test_utils

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
)

func Arr32(b byte) [32]byte {
	var a [32]byte
	for i := range a {
		a[i] = b
	}
	return a
}

func Arr96(b byte) [96]byte {
	var a [96]byte
	for i := range a {
		a[i] = b
	}
	return a
}

func SSZSnappyEncode(t *testing.T, msg consensus.Marshaler) []byte {
	t.Helper()

	var buf bytes.Buffer
	_, err := (&consensus.SSZSnappyCodec{}).EncodeGossip(&buf, msg)
	require.NoError(t, err)
	return buf.Bytes()
}
