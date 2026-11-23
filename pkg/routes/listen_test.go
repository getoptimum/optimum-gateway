package routes_test

import (
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/routes"
	"github.com/getoptimum/optimum-gateway/pkg/test_utils"
)

func TestAppRouter_LoopbackBindNotReachableExternally(t *testing.T) {
	external, err := commonnet.ExternalIP()
	require.NoError(t, err)
	if ip := net.ParseIP(external); ip == nil || ip.To4() == nil || ip.IsLoopback() {
		t.Skip("no non-loopback IPv4 interface")
	}
	cfg := &config.AppConfig{TelemetryAddr: "127.0.0.1", TelemetryPort: test_utils.GetFreePortT(t)}
	// No gateway needed: the test only dials TCP and never hits a handler.
	appRouter := routes.NewAppRouter(logger.NewAppSLogger(logger.Error), nil, cfg)
	runErr := make(chan error, 1)
	go func() { runErr <- appRouter.Run(t.Context()) }()
	t.Cleanup(func() { _ = appRouter.Stop() })

	var exited error
	require.Eventually(t, func() bool {
		select {
		case exited = <-runErr:
			return true
		default:
		}
		conn, err := net.DialTimeout("tcp", cfg.TelemetryLocalTarget(), time.Second)
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, exited, "telemetry listener exited")

	_, err = net.DialTimeout("tcp", net.JoinHostPort(external, fmt.Sprint(cfg.TelemetryPort)), time.Second)
	require.Error(t, err, "loopback bind reachable on %s", external)
	if !errors.Is(err, syscall.ECONNREFUSED) {
		t.Skipf("%s is filtered here, cannot prove refusal: %v", external, err)
	}
}
