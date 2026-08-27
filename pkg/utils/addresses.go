package utils

import (
	"github.com/multiformats/go-multiaddr"

	"github.com/getoptimum/optimum-common/pkg/logger"
	commonnet "github.com/getoptimum/optimum-common/pkg/net"
)

func BuildQUICTCPAddr(
	log logger.AppLogger,
	publicIPV4,
	publicIPV6 string,
	listenPort int,
) []multiaddr.Multiaddr {
	addrsTCP := commonnet.MustBuildAdvertisedAddresses(log, publicIPV4, publicIPV6, listenPort)
	addrsQUIC := commonnet.MustBuildAdvertisedQUICAddresses(log, publicIPV4, publicIPV6, listenPort)
	result := make([]multiaddr.Multiaddr, 0, len(addrsTCP)+len(addrsQUIC))
	result = append(result, addrsTCP...)
	result = append(result, addrsQUIC...)
	return result
}
