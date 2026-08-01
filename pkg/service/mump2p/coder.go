package mump2p

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	mp2pengine "github.com/getoptimum/mump2p-protocol/pkg/engine"
	mp2pshm "github.com/getoptimum/mump2p-protocol/pkg/shm"
	"github.com/getoptimum/shm/pkg/shm"
)

// CoderImageVersion is the rlnc-server release the gateway is pinned to. It must
// move together with the tag in the Makefile and the compose files: the coder and
// the pinned mump2p-protocol share a shared-memory wire format.
const CoderImageVersion = "v0.10.0"

// Coder is the RLNC coding surface the node drives. Aliased so it does not read
// as the gateway's own Engine, which is the node surface consumers depend on.
type Coder = mp2pengine.Engine

// CoderSHMBytes is the /dev/shm capacity a lanes-lane coder needs. Every lane is
// one file of shm.ShmSize bytes, and the sidecar and the gateway share them, so
// this is the floor for the /dev/shm they have in common.
func CoderSHMBytes(lanes int) int64 {
	return int64(lanes) * int64(shm.ShmSize)
}

// newSHMCoder attaches to the RLNC coder's shared-memory lanes and builds the
// engine on top of them. The coder runs out of process, so a missing sidecar
// surfaces here as an error rather than as a node that starts and then silently
// drops every publish it is asked to encode.
func newSHMCoder(cfg *mp2pconfig.Config, log *slog.Logger) (Coder, error) {
	shmCfg := cfg.SharedMemoryConfig
	log.Info("attaching RLNC coder",
		"shm_name", shmCfg.SHMName,
		"shm_lanes", shmCfg.SHMLanes,
		"lane_path", shm.ShmPathForLane(0, shmCfg.SHMName),
		"shm_bytes_required", CoderSHMBytes(shmCfg.SHMLanes),
	)

	svc, err := mp2pshm.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("attach RLNC coder shared memory %q: %s: %w",
			shmCfg.SHMName, coderAttachHint(shmCfg, err), err)
	}
	coder, err := mp2pengine.NewEngine(cfg.RLNCConfig, log, svc)
	if err != nil {
		return nil, fmt.Errorf("create RLNC coder: %w", err)
	}
	return coder, nil
}

// coderAttachHint restates an attach failure in the terms an operator can act on.
// Every cause is outside the gateway process: no rlnc-server sidecar, one started
// with a different --name or --lanes, a /dev/shm that is not shared with it, or
// lane files owned by another user.
func coderAttachHint(shmCfg mp2pconfig.SharedMemoryConfig, err error) string {
	lane := shm.ShmPathForLane(0, shmCfg.SHMName)
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && pathErr.Path != "" {
		lane = pathErr.Path
	}

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("no lane file at %s, so no rlnc-server is serving %q with %d lanes; "+
			"run getoptimum/rlnc-server:%s with --name=%s --lanes=%d and share its /dev/shm with the gateway",
			lane, shmCfg.SHMName, shmCfg.SHMLanes, CoderImageVersion, shmCfg.SHMName, shmCfg.SHMLanes)

	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("the lane file at %s is not readable by this process; "+
			"the gateway and rlnc-server must run as the same user", lane)

	case laneUndersized(lane):
		return fmt.Sprintf("the lane file at %s is smaller than the %d bytes a lane must be, so it was not "+
			"created by rlnc-server:%s; stop the sidecar, delete the stale lane files and start it again",
			lane, shm.ShmSize, CoderImageVersion)

	default:
		return fmt.Sprintf("rlnc-server:%s must be serving %d lanes as %q on a /dev/shm of at least %d bytes "+
			"shared with the gateway", CoderImageVersion, shmCfg.SHMLanes, shmCfg.SHMName, CoderSHMBytes(shmCfg.SHMLanes))
	}
}

// laneUndersized reports whether a lane file exists but is too small to map, which
// is what a stale file or a mismatched rlnc-server build looks like from here.
func laneUndersized(lane string) bool {
	info, err := os.Stat(lane)
	return err == nil && info.Size() < int64(shm.ShmSize)
}
