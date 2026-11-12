package mum_p2p_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-gateway/pkg/service/mum_p2p"
)

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg     *mum_p2p.Config
		wantErr string
	}{
		"rejects non-positive listen port": {
			cfg: &mum_p2p.Config{
				ListenPort: 0,
			},
			wantErr: "listen port must be positive",
		},
		"accepts valid config": {
			cfg: &mum_p2p.Config{
				ListenPort: 4001,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
