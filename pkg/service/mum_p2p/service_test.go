package mum_p2p_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	mump2pcfg "github.com/getoptimum/mump2p-protocol/pkg/config"
	shmpb "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/shm"
	rlncpbtypes "github.com/getoptimum/mump2p-protocol/pkg/rlncpb/types"
	rlncshm "github.com/getoptimum/mump2p-protocol/pkg/shm"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// BenchmarkServer
// BenchmarkServer-12    	  108684	     10969 ns/op

// BenchmarkServer-12    	 2832513	       409.5 ns/op
func BenchmarkServer(b *testing.B) {
	psCfg := mump2pcfg.DefaultGossipSubConfig()
	shmSvc, err := rlncshm.New(psCfg)
	require.NoError(b, err)

	reqBz, err := proto.Marshal(&rlncpbtypes.ShardRequest{
		Data:         test_utils.RandBytes(2048),
		MaxShardSize: psCfg.RLNC.MaxShardSize,
		K:            new(psCfg.RLNC.K),
	})
	require.NoError(b, err)

	for i := 0; i < b.N; i++ {
		_, err = shmSvc.ExecuteOp(shmpb.OperationType_OPERATION_TYPE_SHARD, reqBz)
		require.NoError(b, err)
	}
}
