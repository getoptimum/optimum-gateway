package gossipsub_gateway

import (
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"

	commonslices "github.com/getoptimum/optimum-common/pkg/slices"
)

const unknownClient = "unknown"

// knownCLClients bounds the telemetry client label to a fixed set.
var knownCLClients = []string{"lighthouse", "prysm", "teku", "nimbus", "lodestar", "grandine", "erigon", "hermes"}

// clientFamily matches on the agent's first segment ("Lighthouse/v8.3.3/...") so names elsewhere in
// the string can't mislabel it. An identified peer with no agent is "other"; "unknown" means not identified.
func clientFamily(agentVersion string) string {
	name, _, _ := strings.Cut(strings.ToLower(agentVersion), "/")
	if commonslices.ContainsInSlice(knownCLClients, name) {
		return name
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
