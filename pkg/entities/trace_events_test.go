package entities

import (
	"testing"

	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
)

func TestTraceCategories_AreDisjoint(t *testing.T) {
	seen := make(map[MumP2PTraceEventKind]string)
	for name, set := range map[string]map[MumP2PTraceEventKind]struct{}{
		"mesh":  TraceEventsMeshTopology,
		"rpc":   TraceEventsRPC,
		"shard": TraceEventsShard,
	} {
		for ev := range set {
			other, dup := seen[ev]
			require.Falsef(t, dup, "event %v is in both %s and %s categories", ev, other, name)
			seen[ev] = name
		}
	}
}

func TestMumP2PTraceEventKindOf(t *testing.T) {
	tests := []struct {
		name string
		evt  *tracepb.TraceEvent
		want MumP2PTraceEventKind
	}{
		{"add peer", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_AddPeer{}}, MumP2PTraceEventAddPeer},
		{"remove peer", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_RemovePeer{}}, MumP2PTraceEventRemovePeer},
		{"join", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_Join{}}, MumP2PTraceEventJoin},
		{"leave", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_Leave{}}, MumP2PTraceEventLeave},
		{"graft", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_Graft{}}, MumP2PTraceEventGraft},
		{"prune", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_Prune{}}, MumP2PTraceEventPrune},
		{"receive RPC", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_RecvRpc{}}, MumP2PTraceEventRecvRPC},
		{"send RPC", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_SendRpc{}}, MumP2PTraceEventSendRPC},
		{"drop RPC", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_DropRpc{}}, MumP2PTraceEventDropRPC},
		{"publish message", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_PublishMessage{}}, MumP2PTraceEventPublishMessage},
		{"deliver message", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_DeliverMessage{}}, MumP2PTraceEventDeliverMessage},
		{"reject message", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_RejectMessage{}}, MumP2PTraceEventRejectMessage},
		{"duplicate message", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_DuplicateMessage{}}, MumP2PTraceEventDuplicateMessage},
		{"helpful symbol", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_HelpfulSymbol{}}, MumP2PTraceEventHelpfulSymbol},
		{"redundant symbol", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_RedundantSymbol{}}, MumP2PTraceEventRedundantSymbol},
		{"inconsistent symbol", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_InconsistentSymbol{}}, MumP2PTraceEventInconsistentSymbol},
		{"unnecessary symbol", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_UnnecessarySymbol{}}, MumP2PTraceEventUnnecessarySymbol},
		{"unset", &tracepb.TraceEvent{}, MumP2PTraceEventUnknown},
		{"unsupported", &tracepb.TraceEvent{Event: &tracepb.TraceEvent_EncodeError{}}, MumP2PTraceEventUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, MumP2PTraceEventKindOf(tc.evt))
		})
	}
}

func TestOptimumTraceEventSet(t *testing.T) {
	tests := []struct {
		name             string
		mesh, rpc, shard bool
		wantLen          int
		wantContains     []MumP2PTraceEventKind
		wantExcludes     []MumP2PTraceEventKind
	}{
		{
			name:    "all disabled -> empty",
			wantLen: 0,
		},
		{
			name:         "mesh only",
			mesh:         true,
			wantLen:      len(TraceEventsMeshTopology),
			wantContains: []MumP2PTraceEventKind{MumP2PTraceEventGraft, MumP2PTraceEventAddPeer},
			wantExcludes: []MumP2PTraceEventKind{MumP2PTraceEventRecvRPC, MumP2PTraceEventHelpfulSymbol},
		},
		{
			name:         "rpc only excludes the firehose from mesh/shard",
			rpc:          true,
			wantLen:      len(TraceEventsRPC),
			wantContains: []MumP2PTraceEventKind{MumP2PTraceEventRecvRPC, MumP2PTraceEventSendRPC},
			wantExcludes: []MumP2PTraceEventKind{MumP2PTraceEventGraft, MumP2PTraceEventHelpfulSymbol},
		},
		{
			name:         "shard only",
			shard:        true,
			wantLen:      len(TraceEventsShard),
			wantContains: []MumP2PTraceEventKind{MumP2PTraceEventHelpfulSymbol, MumP2PTraceEventPublishMessage},
			wantExcludes: []MumP2PTraceEventKind{MumP2PTraceEventGraft, MumP2PTraceEventRecvRPC},
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
				require.Truef(t, ok, "expected set to contain %v", ev)
			}
			for _, ev := range tc.wantExcludes {
				_, ok := set[ev]
				require.Falsef(t, ok, "expected set to NOT contain %v", ev)
			}
		})
	}
}
