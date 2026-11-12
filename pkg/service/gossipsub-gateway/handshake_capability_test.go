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

func TestHandshakeHandler_Capability(t *testing.T) {
	srv, rig := newHandshakeTestService(t, nil)
	peerID := mustPeerID(t, rig.DefaultPeerID)
	_ = srv.handshakeBuilder()

	cases := map[string]struct {
		gatewayType    commonentities.GatewayType
		scope          string
		wantCanPublish bool
	}{
		"scope publish can publish":         {gatewayType: commonentities.GatewayTypePartner, scope: commonentities.GrantP2PPublish, wantCanPublish: true},
		"scope subscribe-only is read-only": {gatewayType: commonentities.GatewayTypePartner, scope: commonentities.GrantP2PSubscribe, wantCanPublish: false},
		"scope both can publish":            {gatewayType: commonentities.GatewayTypeHermes, scope: commonentities.GrantP2PPublish + " " + commonentities.GrantP2PSubscribe, wantCanPublish: true},
		"unknown grant fails closed":        {gatewayType: commonentities.GatewayTypeHermes, scope: "p2p:frobnicate", wantCanPublish: false},
		"no scope partner publishes":        {gatewayType: commonentities.GatewayTypePartner, scope: "", wantCanPublish: true},
		"no scope hermes publishes":         {gatewayType: commonentities.GatewayTypeHermes, scope: "", wantCanPublish: true},
		"no scope relay publishes":          {gatewayType: commonentities.GatewayTypeRelay, scope: "", wantCanPublish: true},
		"no scope unknown type read-only":   {gatewayType: "some-future-role", scope: "", wantCanPublish: false},
		"no scope empty type read-only":     {gatewayType: "", scope: "", wantCanPublish: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := NewHandshake(srv.cfg.GatewayClusterID, rig.MustSignToken(t, rig.PrivateKey, func(c *jwks_verifier.Claims) {
				c.Type = tc.gatewayType
				c.Scope = tc.scope
			}), version.GetCommitHash())

			capability, err := srv.handshakeHandler(peerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))

			require.NoError(t, err)
			require.Equal(t, tc.wantCanPublish, capability.CanPublish)
		})
	}
}

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

func TestHandshakeHandler_RejectedHandshakeYieldsNoPublishRights(t *testing.T) {
	srv, rig := newHandshakeTestService(t, nil)
	peerID := mustPeerID(t, rig.DefaultPeerID)
	h := srv.handshakeBuilder().(*Handshake)
	h.ClusterID = "different-cluster"

	capability, err := srv.handshakeHandler(peerID, json.NewDecoder(bytes.NewReader(mustMarshalHandshake(t, h))))

	require.Error(t, err)
	require.False(t, capability.CanPublish)
}
