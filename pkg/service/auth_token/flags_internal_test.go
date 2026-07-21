package auth_token

// White-box tests for the flags poll: pollFlagsOnce drives the loop body, so
// covering it covers the 200/404/401 branches without waiting out the ticker.

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestPollFlagsOnce(t *testing.T) {
	t.Run("200AppliesFlagToManagerAndConfig", func(t *testing.T) {
		rig := test_utils.NewAuthTestRig(t)
		disabled := false
		rig.FlagsStatus = http.StatusOK
		rig.PropagationEnabled = &disabled
		cfg := rig.AppCfg(t)
		m, err := New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.NoError(t, err)

		require.False(t, m.pollFlagsOnce(context.Background()), "200 is not terminal")
		require.False(t, m.PropagationEnabled())
		require.False(t, cfg.KeyPropagationEnabled())

		enabled := true
		rig.PropagationEnabled = &enabled
		require.False(t, m.pollFlagsOnce(context.Background()))
		require.True(t, m.PropagationEnabled())
		require.True(t, cfg.KeyPropagationEnabled())
	})

	t.Run("200WithAbsentFieldFailsOpen", func(t *testing.T) {
		rig := test_utils.NewAuthTestRig(t)
		rig.FlagsStatus = http.StatusOK
		cfg := rig.AppCfg(t)
		m, err := New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.NoError(t, err)
		m.applyPropagation(boolPtr(false)) // pretend a previous poll disabled it

		require.False(t, m.pollFlagsOnce(context.Background()))
		require.True(t, m.PropagationEnabled(), "absent field resolves to enabled")
	})

	t.Run("404KeepsLastValueAndIsNotTerminal", func(t *testing.T) {
		rig := test_utils.NewAuthTestRig(t) // FlagsStatus 0 → 404
		cfg := rig.AppCfg(t)
		m, err := New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.NoError(t, err)
		m.applyPropagation(boolPtr(false))

		require.False(t, m.pollFlagsOnce(context.Background()))
		require.False(t, m.PropagationEnabled(), "pre-rollout auth must not flip the flag")
		select {
		case <-m.terminal:
			t.Fatal("404 must not mark the key terminal")
		default:
		}
	})

	t.Run("401IsTerminalAndStopsBothLoops", func(t *testing.T) {
		rig := test_utils.NewAuthTestRig(t)
		rig.FlagsStatus = http.StatusUnauthorized
		cfg := rig.AppCfg(t)
		m, err := New(t.Context(), logger.NewAppSLogger(logger.Debug), cfg)
		require.NoError(t, err)
		m.applyPropagation(boolPtr(false))

		require.True(t, m.pollFlagsOnce(context.Background()), "401 is terminal")
		require.False(t, m.PropagationEnabled(), "terminal keeps the last value")
		select {
		case <-m.terminal:
		default:
			t.Fatal("terminal channel must be closed after a 401")
		}
	})
}

func boolPtr(v bool) *bool { return &v }
