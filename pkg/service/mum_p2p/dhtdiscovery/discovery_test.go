package discovery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	discovery "github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p/dhtdiscovery"
)

func TestGetSuffix(t *testing.T) {
	table := map[string]string{
		"optimum_hoodi_v2":  "70ee094a7afa1571",
		"optimum_hoodi_v3":  "f6330ac32b94f30c",
		"optimum_mainet_v1": "6ace8f5a88425778",
	}
	for k, v := range table {
		require.Equal(t, v, discovery.GetSuffix(k))
	}
}
