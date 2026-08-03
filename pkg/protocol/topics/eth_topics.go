package topics

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/getoptimum/optimum-gateway/pkg/utils"
)

// ethTopicRegexFull matches full topic format /eth2/<fork_digest>/<topic_name>[_index]/ssz_snappy
var ethTopicRegexFull = regexp.MustCompile(`^/eth2/([0-9a-fA-F]{8})/([a-z_]+)(?:_(\d+))?/ssz_snappy$`)

func GetForkDigestFromTopic(topic string) string {
	m := ethTopicRegexFull.FindStringSubmatch(topic)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(m[1])
}

// IsFullEth2Topic returns true for full topic format /eth2/<digest>/<topic>/ssz_snappy.
func IsFullEth2Topic(s string) bool {
	return ethTopicRegexFull.MatchString(strings.TrimSpace(s))
}

// BuildFullTopic returns full topic /eth2/<digest>/<descriptor>/ssz_snappy.
func BuildFullTopic(forkDigest, descriptor string) string {
	return fmt.Sprintf("/eth2/%s/%s/ssz_snappy", strings.ToLower(forkDigest), strings.TrimSpace(descriptor))
}

// MumP2PAggregatedMessages carries attestations to the mump2p mesh packed into
// one message rather than one per subnet, so it is a single fixed topic and not
// one per attestation subnet.
const MumP2PAggregatedMessages = "mump2p_aggregated_messages"

// sizingForkDigest stands in for a real fork digest where only a topic's length
// matters. Every digest is the same eight hex characters, fixed by
// ethTopicRegexFull, so any of them measures the same.
const sizingForkDigest = "00000000"

// MumP2PPublishTopics returns the topics the gateway publishes to the mump2p
// mesh, at the length they have once the fork digest is known.
//
// It exists because a coded symbol has to reserve room for its topic, and the
// reservation is made before any topic is joined: the digest arrives from the
// consensus layer at runtime, but the length does not depend on it. This set is
// the one filterAndBuildEthTopics builds for a mump2p node, and the two must not
// drift apart, since publishing on a topic longer than what was sized for pushes
// every symbol past the datagram budget and onto the stream fallback.
func MumP2PPublishTopics() []string {
	return []string{
		BuildFullTopic(sizingForkDigest, utils.BeaconBlockBase),
		MumP2PAggregatedMessages,
	}
}
