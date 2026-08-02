package mump2p

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	mp2pengine "github.com/getoptimum/mump2p-protocol/pkg/engine"
	rlncpbshm "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/shm"
	rlncpbtypes "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/types"
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
)

const (
	// chunkingPayloadBytes is a payload of the size the gateway actually carries.
	chunkingPayloadBytes = 50449

	// chunkingTopic is a real gossip topic, not the protocol's placeholder.
	chunkingTopic = "/eth2/c6ecb76c/beacon_block/ssz_snappy"

	// wantChunksAtDatagramShardSize is what a chunkingPayloadBytes payload codes
	// into once the coder shards at the size the datagram budget allows.
	//
	// A message reassembles only when every one of its chunks independently
	// reaches full rank, so delivery falls off as p^chunks. The count is pinned
	// here because a shard-size regression is otherwise invisible: nothing fails,
	// messages just stop arriving.
	wantChunksAtDatagramShardSize = 4
)

// TestCoderChunkCountForARepresentativePayload pins how many chunks the coder
// splits a real payload into on the datagram path.
func TestCoderChunkCountForARepresentativePayload(t *testing.T) {
	cfg := &Config{
		ClusterID:      "test-cluster",
		ListenPort:     4321,
		DatagramEnable: true,
		Rotator:        newRotator(t, &commonentities.OptimumConfig{}),
	}

	nodeCfg, err := toNodeConfig(cfg)
	require.NoError(t, err)

	// The same expression coder.go builds the coder from, so the config this
	// exercises is the config a running gateway codes at.
	coder, err := mp2pengine.NewEngine(
		nodeCfg.RLNCConfig,
		slog.New(slog.DiscardHandler),
		shardingSHM{k: int(nodeCfg.K)},
	)
	require.NoError(t, err)

	envs, err := coder.Encode(
		chunkingTopic,
		"chunk-count-test",
		make([]byte, chunkingPayloadBytes),
		rlncpbtypes.CodingType_CODING_TYPE_SYSTEMATIC,
	)
	require.NoError(t, err)
	require.NotEmpty(t, envs)

	require.Equal(t, wantChunksAtDatagramShardSize, envs[0].TotalChunks,
		"a %d byte payload at a %d byte shard size codes into %d chunks",
		chunkingPayloadBytes, nodeCfg.MaxShardSize, envs[0].TotalChunks)
}

// shardingSHM is the out-of-process coder reduced to the three operations Encode
// drives, so a test can observe how a payload chunks without a sidecar. Only the
// shard count matters here; the symbols it returns are systematic and uncoded.
type shardingSHM struct{ k int }

func (s shardingSHM) ExecuteOp(op rlncpbshm.OperationType, payload []byte) ([]byte, error) {
	switch op {
	case rlncpbshm.OperationType_OPERATION_TYPE_SHARD:
		return s.shard(payload)
	case rlncpbshm.OperationType_OPERATION_TYPE_PREPARE_SYMBOLS:
		return s.prepareSymbols(payload)
	case rlncpbshm.OperationType_OPERATION_TYPE_CODE:
		return s.code(payload)
	default:
		return nil, nil
	}
}

func (s shardingSHM) shard(payload []byte) ([]byte, error) {
	var req rlncpbtypes.ShardRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, err
	}

	size := int(req.GetMaxShardSize())
	data := req.GetData()

	var shards [][]byte
	for start := 0; start < len(data); start += size {
		end := min(start+size, len(data))
		shards = append(shards, slices.Clone(data[start:end]))
	}

	// The real sharder pads the shard count out to whole generations, which is
	// what the engine requires of it.
	for len(shards)%s.k != 0 {
		shards = append(shards, make([]byte, size))
	}

	return proto.Marshal(&rlncpbtypes.ShardResponse{Shards: shards})
}

func (s shardingSHM) prepareSymbols(payload []byte) ([]byte, error) {
	var req rlncpbtypes.PrepareSymbolsRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, err
	}

	shards := req.GetData()
	symbols := make([]*rlncpbtypes.Symbol, 0, len(shards))
	for i, shard := range shards {
		coefficients := make([]byte, len(shards))
		coefficients[i] = 1
		symbols = append(symbols, &rlncpbtypes.Symbol{
			Coefficients: coefficients,
			Data:         slices.Clone(shard),
		})
	}

	return proto.Marshal(&rlncpbtypes.PrepareSymbolsResponse{
		Result: rlncpbtypes.NewSymbolSet(symbols...),
	})
}

func (s shardingSHM) code(payload []byte) ([]byte, error) {
	var req rlncpbtypes.CodeRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, err
	}

	symbols := req.GetSymbols().GetSymbols()
	limit := min(int(req.GetN()), len(symbols))

	return proto.Marshal(&rlncpbtypes.CodeResponse{
		Result: rlncpbtypes.NewSymbolSet(symbols[:limit]...),
	})
}
