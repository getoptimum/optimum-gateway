package entities

import (
	"testing"

	"github.com/stretchr/testify/require"

	pboptimum "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

func TestTraceCategories_AreDisjoint(t *testing.T) {
	seen := make(map[pboptimum.TraceEvent_Type]string)
	for name, set := range map[string]map[pboptimum.TraceEvent_Type]struct{}{
		"mesh":  TraceEventsMeshTopology,
		"rpc":   TraceEventsRPC,
		"shard": TraceEventsShard,
	} {
		for ev := range set {
			other, dup := seen[ev]
			require.Falsef(t, dup, "event %s is in both %s and %s categories", ev, other, name)
			seen[ev] = name
		}
	}
}

func TestOptimumTraceEventSet(t *testing.T) {
	tests := []struct {
		name             string
		mesh, rpc, shard bool
		wantLen          int
		wantContains     []pboptimum.TraceEvent_Type
		wantExcludes     []pboptimum.TraceEvent_Type
	}{
		{
			name:    "all disabled -> empty",
			wantLen: 0,
		},
		{
			name:         "mesh only",
			mesh:         true,
			wantLen:      len(TraceEventsMeshTopology),
			wantContains: []pboptimum.TraceEvent_Type{pboptimum.TraceEvent_GRAFT, pboptimum.TraceEvent_ADD_PEER},
			wantExcludes: []pboptimum.TraceEvent_Type{pboptimum.TraceEvent_RECV_RPC, pboptimum.TraceEvent_NEW_SHARD},
		},
		{
			name:         "rpc only excludes the firehose from mesh/shard",
			rpc:          true,
			wantLen:      len(TraceEventsRPC),
			wantContains: []pboptimum.TraceEvent_Type{pboptimum.TraceEvent_RECV_RPC, pboptimum.TraceEvent_SEND_RPC},
			wantExcludes: []pboptimum.TraceEvent_Type{pboptimum.TraceEvent_GRAFT, pboptimum.TraceEvent_NEW_SHARD},
		},
		{
			name:         "shard only",
			shard:        true,
			wantLen:      len(TraceEventsShard),
			wantContains: []pboptimum.TraceEvent_Type{pboptimum.TraceEvent_NEW_SHARD, pboptimum.TraceEvent_PUBLISH_MESSAGE},
			wantExcludes: []pboptimum.TraceEvent_Type{pboptimum.TraceEvent_GRAFT, pboptimum.TraceEvent_RECV_RPC},
		},
		{
			name:    "all enabled -> union of all three",
			mesh:    true,
			rpc:     true,
			shard:   true,
			wantLen: len(TraceEventsMeshTopology) + len(TraceEventsRPC) + len(TraceEventsShard),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			set := OptimumTraceEventSet(tc.mesh, tc.rpc, tc.shard)
			require.Len(t, set, tc.wantLen)
			for _, ev := range tc.wantContains {
				_, ok := set[ev]
				require.Truef(t, ok, "expected set to contain %s", ev)
			}
			for _, ev := range tc.wantExcludes {
				_, ok := set[ev]
				require.Falsef(t, ok, "expected set to NOT contain %s", ev)
			}
		})
	}
}
