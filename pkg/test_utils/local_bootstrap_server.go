package test_utils

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/stretchr/testify/require"

	commonmaps "github.com/getoptimum/optimum-common/pkg/maps"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

type BlockLatencyRequest struct {
	Authorization string
	Payload       entities.LatencyComparator
}

type RegisterGatewayRequest struct {
	Authorization string
	Payload       map[string]string
}

type ExposeNodesRequest struct {
	Authorization string
	Query         url.Values
}

// LocalBootstrapServer is an in-process httptest bootstrap stub for gateway tests.
type LocalBootstrapServer struct {
	rig                *AuthTestRig
	messages           *syncx.RWMap[string, any]
	forksResponse      *syncx.RWMap[string, any]
	accelerateResponse *syncx.RWMap[string, any]
	registerReqs       chan RegisterGatewayRequest
	exposeReqs         chan ExposeNodesRequest
	latencyReqs        chan BlockLatencyRequest
	srv                *httptest.Server
}

func NewLocalBootstrapServerWithRig(t *testing.T, rig *AuthTestRig) *LocalBootstrapServer {
	t.Helper()
	return newLocalBootstrapServer(t, rig)
}

func newLocalBootstrapServer(t *testing.T, rig *AuthTestRig) *LocalBootstrapServer {
	t.Helper()

	messages := syncx.NewRWMap[string, any]()
	forksResponse := syncx.NewRWMap[string, any]()
	accelerateResponse := syncx.NewRWMap[string, any]()
	registerReqs := make(chan RegisterGatewayRequest, 32)
	exposeReqs := make(chan ExposeNodesRequest, 32)
	latencyReqs := make(chan BlockLatencyRequest, 32)
	app := fiber.New()
	app.Get(utils.BootstrapExposeNodesPath, func(c fiber.Ctx) error {
		select {
		case exposeReqs <- ExposeNodesRequest{
			Authorization: strings.Clone(c.Get("Authorization")),
			Query:         mapToURLValues(c.Queries()),
		}:
		default:
		}
		return c.JSON(commonmaps.MapValues(messages.LoadAll()))
	})
	app.Post(utils.BootstrapRegisterPath, func(c fiber.Ctx) error {
		var p map[string]string
		require.NoError(t, json.Unmarshal(c.Body(), &p))
		select {
		case registerReqs <- RegisterGatewayRequest{
			Authorization: strings.Clone(c.Get("Authorization")),
			Payload:       p,
		}:
		default:
		}
		messages.Store(p["gateway_id"], p)
		return nil
	})
	app.Get(utils.BootstrapForkDigestPath, func(c fiber.Ctx) error {
		if err := requireAuth(rig, c); err != nil {
			return err
		}
		return c.JSON(forksResponse.LoadAll())
	})
	app.Get("/api/v2/:chain/accelerate_slots", func(c fiber.Ctx) error {
		if err := requireAuth(rig, c); err != nil {
			return err
		}
		return c.JSON(accelerateResponse.LoadAll())
	})
	app.Post(utils.BootstrapHandleBlockLatencyV2, func(c fiber.Ctx) error {
		var payload entities.LatencyComparator
		require.NoError(t, json.Unmarshal(c.Body(), &payload))
		req := BlockLatencyRequest{
			Authorization: strings.Clone(c.Get("Authorization")),
			Payload:       payload,
		}
		select {
		case latencyReqs <- req:
		default:
			// Keep latency capture best-effort so unrelated tests never block on this test stub.
		}
		return nil
	})

	srv := httptest.NewServer(adaptor.FiberApp(app))
	t.Cleanup(srv.Close)

	return &LocalBootstrapServer{
		rig:                rig,
		srv:                srv,
		forksResponse:      forksResponse,
		accelerateResponse: accelerateResponse,
		messages:           messages,
		registerReqs:       registerReqs,
		exposeReqs:         exposeReqs,
		latencyReqs:        latencyReqs,
	}
}

// requireAuth answers 401 rather than asserting. These run on fiber's handler
// goroutine, where a failed require calls t.FailNow off the test goroutine: Go
// does not support that, and the caller sees a transport error instead of the
// missing header. A real status lets the caller report it.
func requireAuth(rig *AuthTestRig, c fiber.Ctx) error {
	if rig == nil || c.HasHeader("Authorization") {
		return nil
	}
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing Authorization header"})
}

func mapToURLValues(src map[string]string) url.Values {
	dst := make(url.Values, len(src))
	for key, value := range src {
		dst.Set(key, value)
	}
	return dst
}

func (m *LocalBootstrapServer) SetForkResponse(payload map[string]any) {
	m.forksResponse.Replace(payload)
}

func (m *LocalBootstrapServer) SetAccelerateResponse(payload map[string]any) {
	m.accelerateResponse.Replace(payload)
}

func (m *LocalBootstrapServer) SetMessagesResponse(payload map[string]any) {
	m.messages.Replace(payload)
}

func (m *LocalBootstrapServer) URL() string {
	return m.srv.URL
}

func (m *LocalBootstrapServer) WaitBlockLatencyRequest(t *testing.T, timeout time.Duration) BlockLatencyRequest {
	t.Helper()

	select {
	case req := <-m.latencyReqs:
		return req
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for block latency request")
		return BlockLatencyRequest{}
	}
}

func (m *LocalBootstrapServer) WaitRegisterRequest(t *testing.T, timeout time.Duration) RegisterGatewayRequest {
	t.Helper()

	select {
	case req := <-m.registerReqs:
		return req
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for register request")
		return RegisterGatewayRequest{}
	}
}

func (m *LocalBootstrapServer) WaitExposeNodesRequest(t *testing.T, timeout time.Duration) ExposeNodesRequest {
	t.Helper()

	select {
	case req := <-m.exposeReqs:
		return req
	case <-time.After(timeout):
		t.Fatalf("timeout waiting for expose-nodes request")
		return ExposeNodesRequest{}
	}
}

func (m *LocalBootstrapServer) TryBlockLatencyRequest(timeout time.Duration) (BlockLatencyRequest, bool) {
	select {
	case req := <-m.latencyReqs:
		return req, true
	case <-time.After(timeout):
		return BlockLatencyRequest{}, false
	}
}

func (m *LocalBootstrapServer) AssertNoBlockLatencyRequest(t *testing.T, timeout time.Duration) {
	t.Helper()

	select {
	case req := <-m.latencyReqs:
		t.Fatalf("unexpected block latency request: %+v", req)
	case <-time.After(timeout):
	}
}
