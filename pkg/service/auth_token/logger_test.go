package auth_token_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// The first verified mint stamps the JWT subject onto the auth logger.
// A later mint logs there too, with that gateway_id.
func TestMintStampsGatewayID(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	var buf bytes.Buffer

	log := logger.InitLogger([]io.Writer{&buf}, logger.Debug, logger.WithGatewayID("dev-gateway"))
	m, err := auth_token.New(t.Context(), log, rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)
	require.NoError(t, m.MintForTest(t.Context()))

	var minted int
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var got map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &got))
		if got["msg"] != "minted gateway JWT" {
			continue
		}
		minted++
		require.Equal(t, "gw-test", got["gateway_id"])
		require.Equal(t, "auth_token", got["service"])
	}
	require.Equal(t, 2, minted)
}
