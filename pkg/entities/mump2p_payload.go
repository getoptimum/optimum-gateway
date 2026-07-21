package entities

import (
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	pboptimum "github.com/getoptimum/optimum-p2p/optimum-pubsub/pb"
)

type MumP2PCommand uint8

const (
	_ MumP2PCommand = iota // ignore first value by assigning to blank identifier
	MumP2PCommandTraceMumP2P
	MumP2PCommandMessage
)

type MumP2PResponse struct {
	Command    MumP2PCommand
	Message    *commonentities.P2PMessage
	TraceEvent *pboptimum.TraceEvent
}
