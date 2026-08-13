package test_utils

import (
	"context"
	"crypto/rand"
	_ "embed"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonrand "github.com/getoptimum/optimum-common/pkg/rand"
	commontest "github.com/getoptimum/optimum-common/pkg/test_utils"
)

const (
	// attester index in valid_beacon_attestation_31.hex (see internal/utils/topics_test.go payload1)
	TestKnownValidatorIndex      = uint64(997023)
	TestAttestationAttesterCount = 15

	ETHTestTopicBlock       = "/eth2/c6ecb76c/beacon_block/ssz_snappy"
	ETHTestTopicAttestation = "/eth2/c6ecb76c/beacon_attestation_31/ssz_snappy"
	ETHTestTopicAggregate   = "/eth2/c6ecb76c/beacon_aggregate_and_proof/ssz_snappy"
)

var (
	// ValidBeaconBlockMessage hex encoded real message from hoodi /eth2/c6ecb76c/beacon_block/ssz_snappy
	//go:embed test_data/valid_beacon_block_message.hex
	ValidBeaconBlockMessage string

	// ValidBeaconAggregateAndProof hex encoded real message from hoodi /eth2/c6ecb76c/beacon_aggregate_and_proof/ssz_snappy
	//go:embed test_data/valid_beacon_aggregate_and_proof.hex
	ValidBeaconAggregateAndProof string

	// ValidBeaconAttestation31 hex encoded real message from hoodi /eth2/c6ecb76c/beacon_attestation_31/ssz_snappy
	//go:embed test_data/valid_beacon_attestation_31.hex
	ValidBeaconAttestation31 string

	TopicMessages = map[string]string{
		ETHTestTopicBlock:       ValidBeaconBlockMessage,
		ETHTestTopicAttestation: ValidBeaconAttestation31,
		ETHTestTopicAggregate:   ValidBeaconAggregateAndProof,
	}

	// HoodiBeaconBlockMessage1 real message from hoodi encoded to hex
	//go:embed test_data/hoodi_block_1.hex
	HoodiBeaconBlockMessage1 string

	// HoodiBeaconBlockMessage2 real message from hoodi encoded to hex
	//go:embed test_data/hoodi_block_2.hex
	HoodiBeaconBlockMessage2 string

	// HoodiBeaconBlockMessage3 real message from hoodi encoded to hex
	//go:embed test_data/hoodi_block_3.hex
	HoodiBeaconBlockMessage3 string
)

type Container struct {
	Ctx context.Context
	Log logger.AppLogger
}

func GetClean(tb testing.TB) *Container {
	tb.Helper()

	if available := hasRLNCServerSemaphore(tb, "/dev/shm"); !available {
		tb.Fatalf("rlnc server is not running, start it as `make run-rlnc-server`")
	}

	ctx, cancel := context.WithTimeout(tb.Context(), time.Minute*3)
	log := logger.NewAppSLogger(logger.Debug)
	tb.Cleanup(cancel)
	return &Container{
		Ctx: ctx,
		Log: log,
	}
}

func hasRLNCServerSemaphore(tb testing.TB, dir string) bool {
	tb.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(tb, err)

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "go_shm_rlnc_semaphore_mump2p-protocol_lane") {
			return true
		}
	}
	return false
}

func SpawnLocalDeps(t *testing.T) {
	t.Helper()

	rig := NewAuthTestRig(t)
	rig.ValidatorIndexes = TestAttestationAttesterIndexes()
	// Match the default test gateway cluster (GetTestConfig) so mesh handshakes pass
	// the cluster-membership check (#707).
	rig.SetClusterIDs("optimum_hoodi_v0_2")
	bootstrap := NewLocalBootstrapServerWithRig(t, rig)
	bootstrap.SetForkResponse(map[string]any{
		"chain_id":    "hoodi",
		"fork_digest": "c6ecb76c",
		"future_fork": "deadbeef",
	})
	t.Setenv("OPT_REMOTE_BOOTSTRAP_URL", bootstrap.URL())
	rigCfg := rig.AppCfg(t)
	t.Setenv("OPT_API_KEY", rigCfg.APIKey)
	t.Setenv("OPT_ENABLE_AUTH", "true")
	t.Setenv("OPT_JWKS_CACHE_PATH", rigCfg.JWKSCachePath)
	t.Setenv("OPT_REMOTE_AUTH_URL", rigCfg.RemoteAuthURL)
}

func TestAttestationAttesterIndexes() []uint64 {
	indexes := make([]uint64, TestAttestationAttesterCount)
	for i := range indexes {
		indexes[i] = TestKnownValidatorIndex + uint64(i)
	}
	return indexes
}

// GetFreePortT delegates to optimum-common for a free TCP port in tests.
func GetFreePortT(t *testing.T) int {
	t.Helper()
	return commontest.GetFreePortT(t)
}

func IsCIRun(t *testing.T) bool {
	t.Helper()
	return os.Getenv("CI_RUN") != ""
}

func RandBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func TestRand(t *testing.T) uint64 {
	t.Helper()
	r, err := commonrand.RandBetween(10, 1_000_000)
	require.NoError(t, err)
	return uint64(r) //nolint:gosec // ok for test
}

func GenerateIdentity(t *testing.T) (identityKey *identity.IdentityInfo, keyDir string) {
	t.Helper()

	keyDir = t.TempDir()
	_, err := identity.EnsureIdentity(keyDir)
	require.NoError(t, err)
	identityKey, err = identity.ExtractIdentityFromDir(keyDir)
	require.NoError(t, err)
	return identityKey, keyDir
}
