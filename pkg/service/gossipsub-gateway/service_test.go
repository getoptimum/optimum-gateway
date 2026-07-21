package gossipsub_gateway_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	libp2ppubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	chainstate "github.com/getoptimum/optimum-gateway/pkg/protocol/chain_state"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/consensus"
	"github.com/getoptimum/optimum-gateway/pkg/routes"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	gateway "github.com/getoptimum/optimum-gateway/pkg/service/gossipsub-gateway"
	"github.com/getoptimum/optimum-gateway/pkg/service/message_router"
	"github.com/getoptimum/optimum-gateway/pkg/service/telemetry"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// TestGatewayIntegration runs 3 gateway nodes and 3 CL libp2p peers connected to each gateway node
// so we publish message at one CL node and expect to receive it at another CL node,
// message should pass this way: CL node A -> Gateway A -> Gateway B/C -> CL node B/C
func TestGatewayIntegration(t *testing.T) {
	// given
	cnt := test_utils.GetClean(t)
	test_utils.SpawnLocalDeps(t)

	// generate 3 gateways, each of them will have it own CL peer
	gateways := make([]*gateway.Service, 0, 3)
	mockedCLNodes := make([]*test_utils.P2PNode, 0, 3)
	messages := make([]*syncx.RWMap[string, struct{}], 0, 3)
	for i := range 3 {
		clPort := test_utils.GetFreePortT(t)
		clNodeKey, keyDir := test_utils.GenerateIdentity(t)
		clMessages := syncx.NewRWMap[string, struct{}]()
		t.Setenv("OPT_DIRECT_CL_PEERS", fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", clPort, clNodeKey.ID.String()))

		srvGateway, _ := getGatewayNode(cnt.Ctx, t, cnt.Log, fmt.Sprintf("gw%d", i), true)
		require.NoError(t, srvGateway.Run())

		clNode := test_utils.SpawnLocalCLLibP2PNode(cnt.Ctx, t, srvGateway.GetHostInfo(), clPort, keyDir)
		clNode.SubscribeToTopic(cnt.Ctx, t, []string{
			test_utils.ETHTestTopicBlock,
			test_utils.ETHTestTopicAttestation,
		}, func(message *libp2ppubsub.Message) {
			t.Logf("------------------------ cl %d received message %s ------------------------", i, *message.Topic)
			handleMessage(t, clMessages, message)
		})

		messages = append(messages, clMessages)
		gateways = append(gateways, srvGateway)
		mockedCLNodes = append(mockedCLNodes, clNode)
	}

	require.Eventually(t, func() bool {
		for i, gw := range gateways {
			if err := gw.ConnectToPeer(cnt.Ctx, mockedCLNodes[i].GetHostAddress()); err != nil {
				return false
			}
		}
		return true
	}, 20*time.Second, 2*time.Second)

	require.Eventually(t, func() bool {
		for _, gw := range gateways {
			require.True(t, strings.HasPrefix(gw.GetHostInfo().ID.String(), "16"))
			mumPeers, _, _, _ := gw.GetMumP2PPeers()
			clConnected, perTopicPeers, _, _ := gw.GetLibP2PPeers()
			valid := clConnected == 1 && len(perTopicPeers) > 0 && mumPeers == 2
			if !valid {
				return false
			}
		}
		return true
	}, 20*time.Second, 1*time.Second)

	t.Run("beacon block relay", func(t *testing.T) {
		for i := range gateways {
			msg := make([]*syncx.RWMap[string, struct{}], 0, 2)
			for j := range messages {
				if i == j {
					continue
				}
				msg = append(msg, messages[j])
			}
			publishMessages(cnt.Ctx, t, mockedCLNodes[i], test_utils.ETHTestTopicBlock, 45*time.Second, msg...)
		}
	})

	t.Run("attestation aggregation relay", func(t *testing.T) {
		// Aggregation flow: CL -> GW -> aggregator -> mump2p_aggregated_messages -> peer GW -> CL attestation topic.
		for i := range gateways {
			msg := make([]*syncx.RWMap[string, struct{}], 0, 2)
			for j := range messages {
				if i == j {
					continue
				}
				msg = append(msg, messages[j])
			}
			publishMessages(cnt.Ctx, t, mockedCLNodes[i], test_utils.ETHTestTopicAttestation, 90*time.Second, msg...)
		}
	})

	t.Run("each node should be healthy", func(t *testing.T) {
		for i := range gateways {
			stat, code := gateways[i].BuildHealthResponse()
			require.Equal(t, http.StatusOK, code)
			require.Equal(t, stat.Status, "healthy")
		}
	})
}

// blockSignaturePrefix returns the first 16 bytes of a SignedBeaconBlock's
// signature from its raw SSZ. Layout: 4-byte message offset, then the 96-byte
// signature; the test uses these bytes as a per-message dedup key.
func blockSignaturePrefix(raw []byte) []byte {
	if len(raw) < 20 {
		return nil
	}
	return raw[4:20]
}

func handleMessage(t *testing.T, messages *syncx.RWMap[string, struct{}], message *libp2ppubsub.Message) {
	t.Helper()

	var (
		sszEncoder consensus.SSZSnappyCodec
		key        uuid.UUID
		err        error
	)
	switch *message.Topic {
	case test_utils.ETHTestTopicBlock:
		raw, errD := sszEncoder.DecodeGossipRaw(message.Data)
		require.NoError(t, errD)
		key, err = uuid.FromBytes(blockSignaturePrefix(raw))
	case test_utils.ETHTestTopicAttestation:
		var att consensus.SingleAttestation
		require.NoError(t, sszEncoder.DecodeGossip(message.Data, &att))
		key, err = uuid.FromBytes(att.Signature[:16])
	default:
		t.Fatalf("unexpected topic %s", *message.Topic)
	}
	require.NoError(t, err)

	_, ok := messages.Load(key.String())
	require.False(t, ok) // no duplicate messages should be received
	messages.Store(key.String(), struct{}{})
}

// TestGatewayReal lists topics and connected peers/scores (Prysm). Run locally; skipped in CI.
func TestGatewayReal(t *testing.T) {
	if test_utils.IsCIRun(t) {
		t.Skip("debug test for local use only, runs for hours")
	}
	l := logger.NewAppSLogger(logger.Debug)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Hour)
	defer cancel()
	go utils.DumpMemStat(ctx, l, 5*time.Second)

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)
	envList := map[string]string{
		"OPT_TEST_GATEWAY_PORT":   "13844",
		"OPT_TEST_IDENTITY_DIR":   filepath.Join(homeDir, "local_cl", ".gateway_data", "libp2p"),
		"OPT_IDENTITY_MUMP2P_DIR": filepath.Join(homeDir, "local_cl", ".gateway_data", "mump2p", "mump2p"),
		"OPT_JWKS_CACHE_PATH":     "/tmp",
		"OPT_ENABLE_AUTH":         "true",
		"OPT_API_KEY":             os.Getenv("OPT_API_KEY"),
	}
	for k, v := range envList {
		t.Setenv(k, v)
	}
	go func() {
		require.NoError(t, http.ListenAndServe("127.0.0.1:6060", nil))
	}()

	srv, appConf := getGatewayNode(ctx, t, logger.NewAppSLogger(logger.Debug), "local", false)
	require.NoError(t, srv.Run())
	appRouter := routes.NewAppRouter(l, srv, appConf, fmt.Sprintf(":%d", appConf.TelemetryPort))
	go func() {
		require.NoError(t, appRouter.Run(ctx))
	}()

	time.Sleep(10 * time.Hour) // wait for the gateway node to start
}

func publishMessages(
	parentCtx context.Context,
	t *testing.T,
	clP2PNode *test_utils.P2PNode,
	topic string,
	timeout time.Duration,
	messages ...*syncx.RWMap[string, struct{}],
) {
	t.Helper()
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	var (
		sszEncoder  consensus.SSZSnappyCodec
		send        = syncx.NewRWMap[string, struct{}]()
		wg          sync.WaitGroup
		attesterIdx int
	)
	wg.Go(func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		attesterIndexes := test_utils.TestAttestationAttesterIndexes()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				msg := uuid.New()
				signature := make([]byte, 96)
				copy(signature, msg[:])
				rawBytes, _ := hex.DecodeString(test_utils.TopicMessages[topic])
				slot := chainstate.CurrentSlot(time.Now())
				var payload []byte
				switch topic {
				case test_utils.ETHTestTopicBlock:
					payload = patchBlockGossip(t, sszEncoder, rawBytes, slot, msg)
				case test_utils.ETHTestTopicAttestation:
					var att consensus.SingleAttestation
					require.NoError(t, sszEncoder.DecodeGossip(rawBytes, &att))
					att.AttesterIndex = consensus.ValidatorIndex(attesterIndexes[attesterIdx%len(attesterIndexes)])
					attesterIdx++
					copy(att.Signature[:], msg[:])
					att.Data.Slot = consensus.Slot(slot)
					var d bytes.Buffer
					_, err := sszEncoder.EncodeGossip(&d, &att)
					require.NoError(t, err)
					payload = d.Bytes()
				default:
					t.Fatalf("unexpected topic %s", topic)
				}
				send.Store(msg.String(), struct{}{})
				_ = clP2PNode.PublishMessage(ctx, topic, payload)
			}
		}
	})
	require.Eventuallyf(t, func() bool {
		for _, msg := range messages {
			fetchedMessages := msg.LoadAll()
			found := 0
			for _, sendKey := range send.Keys() {
				if _, ok := fetchedMessages[sendKey]; ok {
					found++
				}
			}
			if found < 7 {
				return false
			}
		}
		return true
	}, timeout, 2*time.Second, "all messages should be received %s", topic)
	cancel()
	wg.Wait()
}

// rawSSZ carries pre-marshaled SSZ so the gossip codec can re-encode a patched
// payload without a typed block builder.
type rawSSZ []byte

func (r rawSSZ) MarshalSSZ() ([]byte, error) { return r, nil }

// patchBlockGossip rewrites the slot and the first 16 signature bytes of a gossip-encoded SignedBeaconBlock without decode
// Layout: 4-byte message offset, then the 96-byte signature; slot is the message's first field at that offset.
func patchBlockGossip(t *testing.T, codec consensus.SSZSnappyCodec, gossip []byte, slot uint64, key uuid.UUID) []byte {
	t.Helper()
	raw, err := codec.DecodeGossipRaw(gossip)
	require.NoError(t, err)
	copy(raw[4:20], key[:])
	msgOff := binary.LittleEndian.Uint32(raw[:4])
	binary.LittleEndian.PutUint64(raw[msgOff:msgOff+8], slot)
	var buf bytes.Buffer
	_, err = codec.EncodeGossip(&buf, rawSSZ(raw))
	require.NoError(t, err)
	return buf.Bytes()
}

func getGatewayNode(
	ctx context.Context,
	t *testing.T,
	l logger.AppLogger,
	nodeID string,
	mockDeps bool,
) (*gateway.Service, *config.AppConfig) {
	t.Helper()

	t.Setenv("OPT_GATEWAY_ID", "local_test_srv_"+nodeID)
	cfg := test_utils.GetTestConfig(t)
	return spawnGateway(ctx, t, l, nodeID, cfg, mockDeps), cfg
}

// spawnGateway builds a gateway Service from an already-loaded config. It is shared by
// the env-driven getGatewayNode and the rig-driven getGatewayNodeWithRig.
func spawnGateway(
	ctx context.Context,
	t *testing.T,
	l logger.AppLogger,
	nodeID string,
	cfg *config.AppConfig,
	mockDeps bool,
) *gateway.Service {
	t.Helper()

	var err error
	srvAuth := auth_token.NewDisabled(l)
	if mockDeps || cfg.APIKey != "" {
		srvAuth, err = auth_token.New(t.Context(), l, cfg)
		require.NoError(t, err)
		srvAuth.Start(ctx)
		_, err = srvAuth.Token(t.Context())
		require.NoError(t, err)
	}

	require.NoError(t, cfg.InitRuntime(ctx, l, srvAuth.Chain().String(), cfg.GatewayID, cfg.GatewayType, cfg.OrgID))

	srvMessageRouter, err := message_router.NewService(ctx, cfg, l, srvAuth)
	require.NoError(t, err)

	srv, err := gateway.NewService(
		ctx,
		l.With(logger.WithString("node", nodeID)),
		cfg,
		srvMessageRouter,
		srvAuth,
	)
	require.NoError(t, err)
	telemetry.InitMetrics(ctx, l, cfg)
	t.Cleanup(srv.Stop)
	return srv
}
