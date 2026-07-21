package entities_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/entities"
)

func TestHandshakeHelpers(t *testing.T) {
	t.Run("new handshake stores cluster id", func(t *testing.T) {
		require.Equal(t, "cluster-a", entities.NewHandshake("cluster-a").ClusterID)
	})

	t.Run("validate rejects empty cluster id", func(t *testing.T) {
		err := (&entities.Handshake{}).Validate("cluster-a")
		require.ErrorContains(t, err, "handshake has no cluster ID")
	})

	t.Run("validate rejects mismatched cluster id", func(t *testing.T) {
		err := entities.NewHandshake("cluster-b").Validate("cluster-a")
		require.ErrorContains(t, err, "handshake cluster ID does not match expected")
	})

	t.Run("validate accepts matching cluster id", func(t *testing.T) {
		require.NoError(t, entities.NewHandshake("cluster-a").Validate("cluster-a"))
	})

	t.Run("builds handshake topic", func(t *testing.T) {
		require.Equal(t, "/optimum/v1/handshake", entities.GetHandshakeTopic(entities.HandshakeV1))
	})
}

func TestSourceString(t *testing.T) {
	require.Equal(t, "libp2p", entities.SourceLibP2P.String())
	require.Equal(t, "mump2p", entities.SourceMumP2P.String())
}
