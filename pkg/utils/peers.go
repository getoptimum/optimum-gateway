package utils

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"

	commonnet "github.com/getoptimum/optimum-common/pkg/net"
)

// PeersFromStrings converts a list of peer strings to a list of peer.AddrInfo.
func PeersFromStrings(peerList []string) ([]peer.AddrInfo, error) {
	result := make([]peer.AddrInfo, 0, len(peerList))
	for _, b := range peerList {
		if pA, err := commonnet.AddressInfoFromString(b); err == nil {
			result = append(result, pA) // in case we have pack addrinfo to string via internal helper
			continue
		}

		info, err := peer.AddrInfoFromString(b)
		if err != nil {
			return nil, fmt.Errorf("error parsing peer %q: %w", b, err)
		}
		result = append(result, *info)
	}
	return result, nil
}
