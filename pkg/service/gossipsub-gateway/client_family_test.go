package gossipsub_gateway

import (
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
)

func TestClientFamily(t *testing.T) {
	for agent, want := range map[string]string{
		"Lighthouse/v8.3.3-57c2abc/x86_64-linux": "lighthouse",
		"Prysm/v7.3.1/96d418cd96":                "prysm",
		"teku/teku/v26.10.3/linux-x86_64/-eclipseadoptium-openjdk64bitservervm-java-21": "teku",
		"nimbus":                        "nimbus",
		"lodestar/v1.50.1/abcdef":       "lodestar",
		"Grandine/2.0.6-abc/x86_64":     "grandine",
		"erigon/caplin":                 "erigon",
		"hermes":                        "hermes",
		"go-libp2p/0.48.0":              "other",
		"":                              "other",
		"hermes/v0.1/lighthouse-compat": "hermes",
		"mev-prysm-bridge/v1":           "other",
	} {
		require.Equal(t, want, clientFamily(agent), agent)
	}
}

func TestPeerClientFamilyFromPeerstore(t *testing.T) {
	svc, _ := startReqRespGateway(t)
	// Peers the gateway never connects to, so identify cannot overwrite the agent.
	known, unknown := randomPeerID(t), randomPeerID(t)
	require.NoError(t, svc.hostLibP2P.Peerstore().Put(known, "AgentVersion", "Lighthouse/v8.3.3"))

	require.Equal(t, "lighthouse", svc.peerClientFamily(known))
	require.Equal(t, "unknown", svc.peerClientFamily(unknown), "never identified")

	anonymous := randomPeerID(t)
	require.NoError(t, svc.hostLibP2P.Peerstore().Put(anonymous, "AgentVersion", ""))
	require.Equal(t, "other", svc.peerClientFamily(anonymous), "identified without an agent")
}

func randomPeerID(t *testing.T) peer.ID {
	t.Helper()
	key, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	id, err := peer.IDFromPrivateKey(key)
	require.NoError(t, err)
	return id
}
