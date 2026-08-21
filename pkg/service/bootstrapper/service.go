package bootstrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/getoptimum/optimum-common/pkg/identity"
	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
	"github.com/getoptimum/optimum-common/pkg/syncx"
	"github.com/getoptimum/optimum-gateway/pkg/config"
	"github.com/getoptimum/optimum-gateway/pkg/entities"
	"github.com/getoptimum/optimum-gateway/pkg/protocol/forks"
	"github.com/getoptimum/optimum-gateway/pkg/service/auth_token"
	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

const (
	// exposeNodesAmount we can ask bootstrap server to give us 7 mump2p gateway peers
	// some of them may be unavailable due to not updated
	// until we have 1 working node - we will be able to join the mump2p mesh
	exposeNodesAmount = "7"
)

type Service struct {
	ctx        context.Context
	log        logger.AppLogger
	cfg        *config.AppConfig
	authMgr    *auth_token.Service
	srvForkMgr *forks.Service

	nodeMumP2PStr string // This gateway's mump2p peer ID
	blockEvents   chan *blockArrival
	trackedSlots  *syncx.TTLMap[uint64, *entities.LatencyComparator]

	// block-latency export retry scheduler (issue #90)
	exportMu   sync.Mutex
	exportPend map[uint64]*pendingExport
	exportWake chan struct{}
}

func NewService(
	ctx context.Context,
	log logger.AppLogger,
	cfg *config.AppConfig,
	authMgr *auth_token.Service,
	srvForkMgr *forks.Service,
) *Service {
	srv := &Service{
		ctx:        ctx,
		log:        log.With(logger.WithService("bootstrapper")),
		cfg:        cfg,
		authMgr:    authMgr,
		srvForkMgr: srvForkMgr,

		blockEvents:  make(chan *blockArrival, 1024),
		trackedSlots: syncx.NewTTLMap[uint64, *entities.LatencyComparator](5*time.Minute, 1*time.Minute),

		exportPend: make(map[uint64]*pendingExport),
		exportWake: make(chan struct{}, 1),
	}
	go srv.runBlockLatencyExporter()
	go srv.bgHandleBlockEvents()
	return srv
}

func (s *Service) SetGatewayPeerIDStr(peerIDStr string) {
	s.nodeMumP2PStr = peerIDStr
}

func (s *Service) RegisterAndGetMumP2PPeers() ([]string, error) {
	predictedAddress, publicIP, err := s.predictMumP2PAddrInfo()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Minute)
	defer cancel()

	registerURL := utils.BootstrapRegisterURL(s.cfg.RemoteBootstrapURL)
	payload := entities.GatewayInfo{
		GatewayID:  s.cfg.GatewayID,
		HTTPAddr:   net.JoinHostPort(publicIP, strconv.Itoa(s.cfg.TelemetryPort)),
		P2PAddr:    commonnet.AddressInfoToString(predictedAddress),
		ClusterID:  s.cfg.GatewayClusterID,
		ChainID:    s.srvForkMgr.AppChain().String(),
		Version:    s.cfg.Version,
		CommitHash: s.cfg.CommitHash,
	}
	registerResult, code, err := utils.RetryPostRequest[map[string]string](ctx, registerURL, payload, s.bearerAuthHeader(ctx))
	res, errE := s.exposeGatewayPeers()
	if errE != nil {
		s.log.Error("failed to get mump2p peers from bootstrap server", errE)
	}

	if !utils.IsPostSuccess(code) {
		// we can't get info from bootstrap server - either protocol changed or server is down
		if registerResult != nil {
			payloadBytes, _ := json.Marshal(payload)
			s.log.Info("response from bootstrap server",
				logger.WithString("payload", string(payloadBytes)),
				logger.WithString("response", fmt.Sprintf("%v", *registerResult)),
			)
		}
		if len(res) == 0 {
			// we not able self register to bootstrap
			// we not able get peers list from any source
			// we can't work in such degraded mode, fatal here
			s.log.Error("failed to register gateway and get mump2p peers from bootstrap server", fmt.Errorf("status code: %d, error: %w", code, err))
			return nil, fmt.Errorf("failed to register mump2p peers from bootstrap server")
		}
	} else {
		s.log.Info("registered gateway to bootstrap server",
			logger.WithString("public_ip", publicIP),
			logger.WithString("predicted_peer_id", predictedAddress.ID.String()),
		)
	}
	go s.heartbeatBootstrapServer(registerURL, &payload)

	if res == nil {
		s.log.Error("failed to get mump2p peers from bootstrap server: response is nil", fmt.Errorf("status code: %d, error: %w", code, err))
		return nil, fmt.Errorf("failed to get mump2p peers from bootstrap server")
	}
	s.log.Info("got mump2p peers from bootstrap server", logger.WithInt("peers_count", len(res)))
	result := make([]string, 0, len(res))
	for _, gateway := range res {
		// try parse p2pAddress. if bootnode for some reason will serve corrupted data, mump2p node will not be able to start
		// we can skip such nodes, but we just log error here
		addr, err := commonnet.AddressInfoFromString(gateway.P2PAddr)
		if err != nil {
			s.log.Error("invalid mump2p_addr from bootstrap server, skipping this node",
				fmt.Errorf("gateway_id: %s, mump2p_addr: %s, error: %w", gateway.GatewayID, gateway.P2PAddr, err),
			)
			continue
		}
		// check that no other gateway registered with same gateway_id but different mump2p key
		if gateway.GatewayID == s.cfg.GatewayID && addr.ID.String() != predictedAddress.ID.String() {
			s.log.Error("gateway with same ID already exists", fmt.Errorf("change gateway_id in config"))
			return nil, fmt.Errorf("gateway with same ID already exists")
		}
		result = append(result, gateway.P2PAddr) // we can add self, it will be just excluded in libp2p
	}
	return result, nil
}

// predictMumP2PAddrInfo gets future mump2p node information.
// we can't start the mump2p node without bootnodes.
// so we need to predict our future peer info and register it on bootstrap server to get bootnodes list
// after that start the mump2p node with this bootnodes list.
// if we fail to predict our future peer info, we can't start the mump2p node at all, so we fatal here
func (s *Service) predictMumP2PAddrInfo() (peerInfo peer.AddrInfo, publicIP string, err error) {
	publicIPV4, publicIPV6, err := commonnet.GetExternalIPs()
	if err != nil {
		s.log.Error("failed to get public IP, falling back to interface IP", err)
		publicIPV4, err = commonnet.ExternalIP()
		if err != nil {
			return peer.AddrInfo{}, "", fmt.Errorf("unable to get any IP address: %w", err)
		}
	}
	s.log.Info("public IP address detected from prediction",
		logger.WithString("public_ip", publicIPV4),
		logger.WithString("public_ip_v6", publicIPV6),
	)

	if _, err = identity.EnsureIdentity(s.cfg.IdentityMumP2PDir); err != nil {
		s.log.Error("unable to ensure identity info", err)
		return peer.AddrInfo{}, "", fmt.Errorf("unable to ensure identity info: %w", err)
	}
	identityKey, err := identity.ExtractIdentityFromDir(s.cfg.IdentityMumP2PDir)
	if err != nil {
		s.log.Error("unable to load identity info", err)
		return peer.AddrInfo{}, "", fmt.Errorf("unable to load identity info: %w", err)
	}
	publicHost := publicIPV4
	if strings.TrimSpace(publicHost) == "" {
		publicHost = publicIPV6
	}
	if strings.TrimSpace(publicHost) == "" {
		s.log.Error("unable to determine public IP for bootstrap registration", fmt.Errorf("empty IPv4 and IPv6"))
		return peer.AddrInfo{}, "", fmt.Errorf("empty IPv4 and IPv6")
	}
	return peer.AddrInfo{
		ID:    identityKey.ID,
		Addrs: commonnet.MustBuildAdvertisedAddresses(s.log, publicIPV4, publicIPV6, s.cfg.AgentMumP2PPort),
	}, publicHost, nil
}

// exposeGatewayPeers tries to get peers list from bootstrap server and fallback to GitHub raw if it fails
func (s *Service) exposeGatewayPeers() ([]*entities.GatewayInfo, error) {
	exposeURLs := []struct {
		url  string
		auth bool
	}{
		{
			url:  utils.BootstrapExposeNodesURL(s.cfg.RemoteBootstrapURL, s.cfg.GatewayClusterID, s.cfg.Version, exposeNodesAmount),
			auth: true,
		},
		{
			// fallback to GitHub raw in case bootstrap server is down or not updated
			url: utils.ForkDigestHubPeersRawURL(s.srvForkMgr.AppChain(), s.cfg.GatewayClusterID),
		},
	}
	for _, exposeURL := range exposeURLs {
		ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
		defer cancel() //nolint:gocritic // no leak, we just have 2 items in loop
		var headers map[string]string
		if exposeURL.auth {
			headers = s.bearerAuthHeader(ctx)
		}
		res, code, err := utils.RetryGetRequest[[]*entities.GatewayInfo](ctx, exposeURL.url, headers)
		if code == http.StatusOK && res != nil && len(*res) > 0 {
			return *res, nil
		}
		s.log.Error("failed to get gateway peers from url", fmt.Errorf("status code: %d, error: %w", code, err))
	}
	return nil, fmt.Errorf("failed to expose all peers from bootstrap server")
}

// bearerAuthHeader returns an Authorization header for centralized HTTP calls
func (s *Service) bearerAuthHeader(ctx context.Context) map[string]string {
	tok, err := s.authMgr.ServicesToken(ctx)
	if err != nil || tok == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + tok}
}
