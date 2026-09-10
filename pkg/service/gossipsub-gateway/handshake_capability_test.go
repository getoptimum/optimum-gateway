package gossipsub_gateway

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	"github.com/getoptimum/optimum-common/pkg/version"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/service/jwks_verifier"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

// A peer's capability is derived from its verified `type` claim and nothing else, via
// commonentities.GatewayType.CanPublish so that billing, auth and the gateway share one
// definition. That helper is fail-CLOSED: only hermes/partner/relay publish, while stream,
// empty and unrecognized roles do not. The unknown/empty cases below pin that deliberately —
// they are safe only because the rollout rule forbids minting a role before the whole fleet
// understands it. If these ever need to flip back to publish, the rollout rule changed.
func TestHandshakeHandler_Capability(t *testing.T) {
	srv, rig := newHandshakeTestService(t, nil)
	peerID := mustPeerID(t, rig.DefaultPeerID)
	_ = srv.handshakeBuilder() // prime our own claims so the chain check passes

	cases := map[string]struct {
		gatewayType    commonentities.GatewayType
		wantCanPublish bool
	}{
		"stream is read-only":       {gatewayType: commonentities.GatewayTypeStream, wantCanPublish: false},
		"partner can publish":       {gatewayType: commonentities.GatewayTypePartner, wantCanPublish: true},
		"hermes can publish":        {gatewayType: commonentities.GatewayTypeHermes, wantCanPublish: true},
		"relay can publish":         {gatewayType: commonentities.GatewayTypeRelay, wantCanPublish: true},
		"unknown type fails closed": {gatewayType: "some-future-role", wantCanPublish: false},
		"empty type fails closed":   {gatewayType: "", wantCanPublish: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
				c.Type = tc.gatewayType
			}), version.GetCommitHash())

			capability, err := srv.handshakeHandler(peerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))

			require.NoError(t, err)
			require.Equal(t, tc.wantCanPublish, capability.CanPublish)
		})
	}
}

// With auth disabled there is no verified role to read, so admission must stay exactly
// as it was before capabilities existed: full publish.
func TestHandshakeHandler_CapabilityWhenAuthDisabled(t *testing.T) {
	disabled, _ := newHandshakeTestService(t, func(_ *test_utils.AuthTestRig, cfg *config.AppConfig) {
		cfg.EnableAuth = false
	})
	h := NewHandshake(disabled.cfg.GatewayClusterID, "", version.GetCommitHash())
	otherPeerID := mustPeerIDFromIdentityDir(t, t.TempDir())

	capability, err := disabled.handshakeHandler(otherPeerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))

	require.NoError(t, err)
	require.True(t, capability.CanPublish)
}

// A rejected handshake must not hand back a usable capability: the caller disconnects,
// and the zero value it receives is read-only rather than publish-capable.
func TestHandshakeHandler_RejectedHandshakeYieldsNoPublishRights(t *testing.T) {
	srv, rig := newHandshakeTestService(t, nil)
	peerID := mustPeerID(t, rig.DefaultPeerID)
	h := srv.handshakeBuilder().(*Handshake)
	h.ClusterID = "different-cluster"

	capability, err := srv.handshakeHandler(peerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))

	require.Error(t, err)
	require.False(t, capability.CanPublish)
}
