package test_utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	mp2pconfig "github.com/getoptimum/mump2p-protocol/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/mump2p"
	"github.com/getoptimum/shm/pkg/shm"
)

// EnvRunDatagramE2E opts into the sidecar-backed end-to-end suite. It is unset
// everywhere by default, so `go test ./...` on a plain checkout never reaches
// for a container runtime; see envRunGatewayReal for the same convention.
const EnvRunDatagramE2E = "OPT_RUN_DATAGRAM_E2E"

const (
	// sidecarReadyTimeout bounds the wait for the coder to map its lane set.
	sidecarReadyTimeout = 60 * time.Second
	sidecarPollInterval = 250 * time.Millisecond

	// dockerCmdTimeout bounds a single docker invocation. Only the image pull is
	// allowed longer, since it may cross the network.
	dockerCmdTimeout  = 30 * time.Second
	dockerPullTimeout = 5 * time.Minute
)

// RLNCSidecar is a running getoptimum/rlnc-server container and the
// shared-memory settings a gateway node has to be configured with to reach it.
//
// The gateway cannot link the real coder in process: rlnc and mump2p-protocol's
// vendored rlncpb register the same protobuf descriptors, so a binary carrying
// both panics at init. Running the coder out of process is therefore the only
// way any test drives the real Galois-field arithmetic rather than a stand-in.
type RLNCSidecar struct {
	SHMName  string
	SHMLanes int
}

// RequireRLNCSidecar starts a coder sidecar for the duration of the test, or
// skips when this machine cannot run one.
//
// Every reason to skip is an environment fact, never a defect in the code under
// test: the opt-in is unset, there is no container runtime, /dev/shm cannot hold
// the lane set, or the pinned image is neither cached nor pullable.
func RequireRLNCSidecar(t *testing.T) *RLNCSidecar {
	t.Helper()

	if os.Getenv(EnvRunDatagramE2E) == "" {
		t.Skipf("set %s=1 to run; it starts a getoptimum/rlnc-server:%s container and needs a shared /dev/shm",
			EnvRunDatagramE2E, mump2p.CoderImageVersion)
	}

	requireDocker(t)

	lanes := mp2pconfig.DefaultSharedMemoryConfig().SHMLanes
	requireSHMCapacity(t, lanes)

	image := fmt.Sprintf("getoptimum/rlnc-server:%s", mump2p.CoderImageVersion)
	requireImage(t, image)

	// The name scopes the lane files, so a per-run one keeps this sidecar clear
	// of any coder a developer already has attached to the same /dev/shm.
	shmName := fmt.Sprintf("optimum-gateway-e2e-%d", time.Now().UnixNano())
	container := "optimum-gateway-e2e-coder-" + shmName

	args := []string{"run", "-d", "--name", container}
	// The gateway and the coder must own the lane files as the same user. A
	// rootful daemon maps container UIDs straight through, so the process UID is
	// passed; a rootless one already maps container root to it, and passing the
	// UID there would land on a subuid instead.
	if !rootlessRuntime() {
		args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	args = append(args,
		"-v", "/dev/shm:/dev/shm",
		image,
		"--name="+shmName,
		fmt.Sprintf("--lanes=%d", lanes),
		"--output-reclaim-after=5s",
	)

	if out, err := runDocker(dockerCmdTimeout, args...); err != nil {
		t.Skipf("cannot start the coder sidecar: %v: %s", err, out)
	}

	sidecar := &RLNCSidecar{SHMName: shmName, SHMLanes: lanes}
	t.Cleanup(func() {
		if out, err := runDocker(dockerCmdTimeout, "logs", "--tail", "20", container); err == nil && t.Failed() {
			t.Logf("--- coder sidecar logs ---\n%s--- end coder sidecar logs ---", out)
		}
		if _, err := runDocker(dockerCmdTimeout, "rm", "-f", container); err != nil {
			t.Logf("failed to remove coder sidecar %s: %v", container, err)
		}
		// The coder does not unlink its lanes, and 20 of them is 320 MiB of
		// tmpfs, so a suite that left them behind would exhaust /dev/shm.
		sidecar.removeLanes(t)
	})

	sidecar.waitReady(t)
	return sidecar
}

// waitReady blocks until every lane file is mapped, full sized and readable by
// this process. Lanes are created in order, so the last one is the whole set.
func (s *RLNCSidecar) waitReady(t *testing.T) {
	t.Helper()

	last := shm.ShmPathForLane(s.SHMLanes-1, s.SHMName)
	deadline := time.Now().Add(sidecarReadyTimeout)
	for {
		info, err := os.Stat(last)
		if err == nil && info.Size() == int64(shm.ShmSize) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("coder sidecar did not map %d lanes as %q within %s (last lane %s: %v)",
				s.SHMLanes, s.SHMName, sidecarReadyTimeout, last, err)
		}
		time.Sleep(sidecarPollInterval)
	}

	// Ownership, not just existence: an unreadable lane is the failure mode of a
	// mismatched container user, and it must be named here rather than surface
	// later as an opaque attach error.
	first := shm.ShmPathForLane(0, s.SHMName)
	f, err := os.Open(first)
	if err != nil {
		t.Fatalf("the coder's lane file %s is not readable by this process (uid %d); "+
			"the gateway and rlnc-server must run as the same user: %v", first, os.Getuid(), err)
	}
	_ = f.Close()
}

func (s *RLNCSidecar) removeLanes(t *testing.T) {
	t.Helper()

	for i := range s.SHMLanes {
		lane := shm.ShmPathForLane(i, s.SHMName)
		if err := os.Remove(lane); err != nil && !os.IsNotExist(err) {
			t.Logf("failed to remove lane file %s: %v", lane, err)
		}
	}
}

func requireDocker(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skipf("no docker binary on PATH: %v", err)
	}
	// Plain `docker info`, not a formatted one: podman answers the same command
	// with a different schema, and only the exit status means "daemon reachable"
	// on both.
	if out, err := runDocker(dockerCmdTimeout, "info"); err != nil {
		t.Skipf("no reachable container runtime: %v: %s", err, out)
	}
}

// requireSHMCapacity skips unless /dev/shm can hold the lane set. Docker's
// default 64 MiB is nowhere near it, so this is a real failure mode rather than
// a theoretical one.
func requireSHMCapacity(t *testing.T, lanes int) {
	t.Helper()

	need := mump2p.CoderSHMBytes(lanes)
	free, err := shmFreeBytes()
	if err != nil {
		t.Skipf("cannot size the shared memory the coder needs: %v", err)
	}
	if free < need {
		t.Skipf("/dev/shm has %d free bytes but %d lanes need %d; mount a larger tmpfs", free, lanes, need)
	}
}

// requireImage makes sure the pinned coder image is available locally, pulling
// it once if it is not.
func requireImage(t *testing.T, image string) {
	t.Helper()

	if _, err := runDocker(dockerCmdTimeout, "image", "inspect", image); err == nil {
		return
	}
	if out, err := runDocker(dockerPullTimeout, "pull", image); err != nil {
		t.Skipf("coder image %s is neither cached nor pullable: %v: %s", image, err, out)
	}
}

// rootlessRuntime reports whether the container runtime maps container root to
// the calling user. Podman and Docker report it under different keys, and a
// template that names a missing field errors, so a failed probe simply means
// "not that runtime".
func rootlessRuntime() bool {
	if out, err := runDocker(dockerCmdTimeout, "info", "--format", "{{.Host.Security.Rootless}}"); err == nil {
		if strings.TrimSpace(out) == "true" {
			return true
		}
	}
	out, err := runDocker(dockerCmdTimeout, "info", "--format", "{{range .SecurityOptions}}{{.}} {{end}}")
	return err == nil && strings.Contains(out, "rootless")
}

func runDocker(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// shmFreeBytes reports the free space on the filesystem the coder's lane files
// live on, which is the /dev/shm the gateway and the sidecar share.
func shmFreeBytes() (int64, error) {
	dir := filepath.Dir(shm.ShmPathForLane(0, "capacity-probe"))

	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	//nolint:gosec // block counts and sizes are non-negative by construction
	return int64(uint64(st.Bsize) * st.Bavail), nil
}
