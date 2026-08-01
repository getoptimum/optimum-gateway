package entities

import (
	"testing"

	"github.com/stretchr/testify/require"

	tracepb "github.com/getoptimum/mump2p-protocol/pkg/pb"
)

func TestTraceCategoriesEnabled(t *testing.T) {
	mesh := &tracepb.TraceEvent{Event: &tracepb.TraceEvent_Graft{Graft: &tracepb.Graft{}}}
	rpc := &tracepb.TraceEvent{Event: &tracepb.TraceEvent_RecvRpc{RecvRpc: &tracepb.RecvRPC{}}}
	shard := &tracepb.TraceEvent{
		Event: &tracepb.TraceEvent_HelpfulSymbol{HelpfulSymbol: &tracepb.SymbolContainer{}},
	}

	tests := []struct {
		name                       string
		cats                       TraceCategories
		wantMesh, wantRPC, wantSha bool
	}{
		{name: "all disabled", cats: NewTraceCategories(false, false, false)},
		{name: "mesh only", cats: NewTraceCategories(true, false, false), wantMesh: true},
		{name: "rpc only", cats: NewTraceCategories(false, true, false), wantRPC: true},
		{name: "shard only", cats: NewTraceCategories(false, false, true), wantSha: true},
		{
			name:     "all enabled",
			cats:     NewTraceCategories(true, true, true),
			wantMesh: true, wantRPC: true, wantSha: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantMesh, tc.cats.Enabled(mesh))
			require.Equal(t, tc.wantRPC, tc.cats.Enabled(rpc))
			require.Equal(t, tc.wantSha, tc.cats.Enabled(shard))
			require.Equal(t, tc.wantMesh || tc.wantRPC || tc.wantSha, tc.cats.Any())
		})
	}
}

func TestTraceCategoriesIgnoresUnclassifiableEvents(t *testing.T) {
	all := NewTraceCategories(true, true, true)

	require.False(t, all.Enabled(nil))
	require.False(t, all.Enabled(&tracepb.TraceEvent{}))
}

func TestTraceCategoriesCoversEveryEventKind(t *testing.T) {
	all := NewTraceCategories(true, true, true)

	// Any oneof variant the protocol emits must land in exactly one category;
	// an unmapped variant would silently stop being fanned out.
	events := []*tracepb.TraceEvent{
		{Event: &tracepb.TraceEvent_PublishMessage{PublishMessage: &tracepb.PublishMessage{}}},
		{Event: &tracepb.TraceEvent_RejectMessage{RejectMessage: &tracepb.RejectMessage{}}},
		{Event: &tracepb.TraceEvent_DuplicateMessage{DuplicateMessage: &tracepb.DuplicateMessage{}}},
		{Event: &tracepb.TraceEvent_DeliverMessage{DeliverMessage: &tracepb.DeliverMessage{}}},
		{Event: &tracepb.TraceEvent_AddPeer{AddPeer: &tracepb.AddPeer{}}},
		{Event: &tracepb.TraceEvent_RemovePeer{RemovePeer: &tracepb.RemovePeer{}}},
		{Event: &tracepb.TraceEvent_RecvRpc{RecvRpc: &tracepb.RecvRPC{}}},
		{Event: &tracepb.TraceEvent_SendRpc{SendRpc: &tracepb.SendRPC{}}},
		{Event: &tracepb.TraceEvent_DropRpc{DropRpc: &tracepb.DropRPC{}}},
		{Event: &tracepb.TraceEvent_Join{Join: &tracepb.Join{}}},
		{Event: &tracepb.TraceEvent_Leave{Leave: &tracepb.Leave{}}},
		{Event: &tracepb.TraceEvent_Graft{Graft: &tracepb.Graft{}}},
		{Event: &tracepb.TraceEvent_Prune{Prune: &tracepb.Prune{}}},
		{Event: &tracepb.TraceEvent_HelpfulSymbol{HelpfulSymbol: &tracepb.SymbolContainer{}}},
		{Event: &tracepb.TraceEvent_RedundantSymbol{RedundantSymbol: &tracepb.SymbolContainer{}}},
		{Event: &tracepb.TraceEvent_InconsistentSymbol{InconsistentSymbol: &tracepb.SymbolContainer{}}},
		{Event: &tracepb.TraceEvent_UnnecessarySymbol{UnnecessarySymbol: &tracepb.SymbolContainer{}}},
		{Event: &tracepb.TraceEvent_EncodeError{EncodeError: &tracepb.EncodeError{}}},
		{Event: &tracepb.TraceEvent_Recode{Recode: &tracepb.Recode{}}},
		{Event: &tracepb.TraceEvent_ChunkDecoded{ChunkDecoded: &tracepb.ChunkDecoded{}}},
	}
	for _, evt := range events {
		require.Truef(t, all.Enabled(evt), "event %T is not mapped to any category", evt.GetEvent())
	}
}
