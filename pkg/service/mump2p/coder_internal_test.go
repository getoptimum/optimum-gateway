package mump2p

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	"github.com/getoptimum/shm/pkg/shm"
)

func testSHMConfig() mp2pconfig.SharedMemoryConfig {
	return mp2pconfig.SharedMemoryConfig{SHMName: "optimum-gateway-coder", SHMLanes: 20}
}

// The lane-set size is what /dev/shm has to be sized for, so it is asserted rather
// than left implicit in the compose files.
func TestCoderSHMBytesIsTheWholeLaneSet(t *testing.T) {
	require.Equal(t, int64(shm.ShmSize), CoderSHMBytes(1))
	require.Equal(t, int64(20)*int64(shm.ShmSize), CoderSHMBytes(20))
	require.Greater(t, CoderSHMBytes(20), int64(64*1024*1024), "the default 64 MiB /dev/shm must not look sufficient")
}

func TestCoderAttachHintMissingLaneNamesTheSidecar(t *testing.T) {
	cfg := testSHMConfig()
	missing := shm.ShmPathForLane(7, cfg.SHMName)
	err := fmt.Errorf("attach lane set: %w", &fs.PathError{Op: "open", Path: missing, Err: fs.ErrNotExist})

	hint := coderAttachHint(cfg, err)
	require.Contains(t, hint, missing, "the hint must name the lane that was missing, not always lane 0")
	require.Contains(t, hint, "getoptimum/rlnc-server:"+CoderImageVersion)
	require.Contains(t, hint, "--lanes=20")
	require.Contains(t, hint, "--name="+cfg.SHMName)
}

func TestCoderAttachHintPermissionPointsAtTheUser(t *testing.T) {
	cfg := testSHMConfig()
	lane := shm.ShmPathForLane(0, cfg.SHMName)
	err := fmt.Errorf("attach lane set: %w", &fs.PathError{Op: "open", Path: lane, Err: fs.ErrPermission})

	hint := coderAttachHint(cfg, err)
	require.Contains(t, hint, lane)
	require.Contains(t, hint, "same user")
}

func TestCoderAttachHintUndersizedLaneReportsAStaleSegment(t *testing.T) {
	cfg := mp2pconfig.SharedMemoryConfig{SHMName: "optimum-gateway-stale-" + t.Name(), SHMLanes: 20}
	lane := shm.ShmPathForLane(0, cfg.SHMName)
	if err := os.WriteFile(lane, []byte("too small"), 0o600); err != nil {
		t.Skipf("cannot stage a lane file at %s: %v", lane, err)
	}
	t.Cleanup(func() { _ = os.Remove(lane) })

	hint := coderAttachHint(cfg, errors.New("attach lane set: attach lane 0 failed: file too small"))
	require.Contains(t, hint, lane)
	require.Contains(t, hint, "stale lane files")
}

func TestCoderAttachHintFallsBackToTheFullRequirement(t *testing.T) {
	cfg := testSHMConfig()

	hint := coderAttachHint(cfg, errors.New("mmap failed: cannot allocate memory"))
	require.Contains(t, hint, cfg.SHMName)
	require.Contains(t, hint, fmt.Sprintf("%d bytes", CoderSHMBytes(cfg.SHMLanes)))
}

func TestLaneUndersizedIgnoresAbsentAndFullSizeLanes(t *testing.T) {
	require.False(t, laneUndersized(shm.ShmPathForLane(0, "optimum-gateway-absent-"+t.Name())))

	lane := shm.ShmPathForLane(0, "optimum-gateway-sized-"+t.Name())
	f, err := os.OpenFile(lane, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Skipf("cannot stage a lane file at %s: %v", lane, err)
	}
	t.Cleanup(func() { _ = os.Remove(lane) })
	require.NoError(t, f.Truncate(int64(shm.ShmSize)))
	require.NoError(t, f.Close())

	require.False(t, laneUndersized(lane))
}
