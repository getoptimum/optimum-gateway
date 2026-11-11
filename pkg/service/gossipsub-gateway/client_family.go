package gossipsub_gateway

import (
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/identify"
)

// identifyWaitTimeout bounds how long goodbye attribution waits for an identify still in flight.
const identifyWaitTimeout = 2 * time.Second

const unknownClient = "unknown"

// knownCLClients bounds the telemetry client label to a fixed set.
var knownCLClients = []string{"lighthouse", "prysm", "teku", "nimbus", "lodestar", "grandine", "erigon", "hermes"}

// clientFamily matches on the agent's first segment ("Lighthouse/v8.3.3/...") so names elsewhere in
// the string can't mislabel it. An identified peer with no agent is "other"; "unknown" means not identified.
func clientFamily(agentVersion string) string {
	name, _, _ := strings.Cut(strings.ToLower(agentVersion), "/")
	for _, client := range knownCLClients {
		if name == client {
			return client
		}
	}
	return "other"
}

func (s *Service) peerClientFamily(p peer.ID) string {
	agent, err := s.hostLibP2P.Peerstore().Get(p, "AgentVersion")
	if err != nil {
		return unknownClient
	}
	v, _ := agent.(string)
	return clientFamily(v)
}

// waitIdentified lets a peer that says goodbye right after connecting still be attributed.
func (s *Service) waitIdentified(conn network.Conn) {
	ids, ok := s.hostLibP2P.(interface{ IDService() identify.IDService })
	if !ok {
		return
	}
	timer := time.NewTimer(identifyWaitTimeout)
	defer timer.Stop()
	select {
	case <-ids.IDService().IdentifyWait(conn):
	case <-timer.C:
	case <-s.ctx.Done():
	}
}
