package mump2p

import (
	"fmt"
	"log/slog"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	mp2pengine "github.com/getoptimum/mump2p-protocol/pkg/engine"
	mp2pshm "github.com/getoptimum/mump2p-protocol/pkg/shm"
)

// Coder is the RLNC coding surface the node drives. Aliased so it does not read
// as the gateway's own Engine, which is the node surface consumers depend on.
type Coder = mp2pengine.Engine

// newSHMCoder attaches to the RLNC coder's shared-memory lanes and builds the
// engine on top of them. The coder runs out of process, so a missing sidecar
// surfaces here as an error rather than as a node that starts and then silently
// drops every publish it is asked to encode.
func newSHMCoder(cfg *mp2pconfig.Config, log *slog.Logger) (Coder, error) {
	svc, err := mp2pshm.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("attach RLNC coder shared memory %q: %w", cfg.SHMName, err)
	}
	coder, err := mp2pengine.NewEngine(cfg.RLNCConfig, log, svc)
	if err != nil {
		return nil, fmt.Errorf("create RLNC coder: %w", err)
	}
	return coder, nil
}
