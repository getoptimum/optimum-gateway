package auth_token_test

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// syncBuffer is a log sink the test can read while a logger may still write to it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// main builds the final logger only after the first mint and then hands it over.
// A later mint must log there, with that logger's gateway_id, and not on the boot logger.
func TestSetLogger_LaterLogsUseTheNewLogger(t *testing.T) {
	rig := test_utils.NewAuthTestRig(t)
	var boot, final syncBuffer

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

// main swaps the logger while the dynamic-config poll may already be logging.
// Run with -race.
func TestSetLogger_SafeWhileLogging(t *testing.T) {
	m, _ := newMintedManager(t, nil)

	var wg sync.WaitGroup
	wg.Go(func() {
		for range 20 {
			if err := m.MintForTest(t.Context()); err != nil {
				t.Error(err)
				return
			}
		}
	})
	for range 20 {
		m.SetLogger(logger.InitLogger([]io.Writer{io.Discard}, logger.Debug))
	}
	wg.Wait()
}
