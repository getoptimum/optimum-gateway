package test_utils

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

func TestLocalBootstrapServesDynamicConfig(t *testing.T) {
	bootstrap := NewLocalBootstrapServerWithRig(t, nil)
	res, err := http.Get(bootstrap.URL() + "/api/v1/hoodi/optimum_hoodi_v0_2/config?service_version=gateway-dev")
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	var got commonentities.DynamicConfig
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, "hoodi", got.ChainID)
	require.Equal(t, "optimum_hoodi_v0_2", got.ClusterID)
	require.Equal(t, "gateway-dev", got.ServiceVersion)
	require.NoError(t, got.Validate())
}
