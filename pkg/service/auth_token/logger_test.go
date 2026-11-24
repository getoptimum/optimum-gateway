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

// main builds the final logger only after the first mint and then hands it over.
// A later mint must log there, with that logger's gateway_id, and not on the boot logger.
func TestSetLogger_LaterLogsUseTheNewLogger(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	var boot, final bytes.Buffer

	bootLog := logger.InitLogger([]io.Writer{&boot}, logger.Debug, logger.WithGatewayID("dev-gateway"))
	m, err := auth_token.New(t.Context(), bootLog, rig.AppCfg(t))
	require.NoError(t, err)
	_, err = m.Token(t.Context())
	require.NoError(t, err)
	bootLines := boot.String()
	require.Contains(t, bootLines, "minted gateway JWT", "the first mint logs on the boot logger")

	m.SetLogger(logger.InitLogger([]io.Writer{&final}, logger.Debug, logger.WithGatewayID("gw-test")))
	require.NoError(t, m.MintForTest(t.Context()))

	require.Equal(t, bootLines, boot.String(), "nothing reaches the boot logger after SetLogger")
	lines := strings.Split(strings.TrimSpace(final.String()), "\n")
	require.Len(t, lines, 1)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &got))
	require.Equal(t, "minted gateway JWT", got["msg"])
	require.Equal(t, "gw-test", got["gateway_id"])
	require.Equal(t, "auth_token", got["service"])
}
