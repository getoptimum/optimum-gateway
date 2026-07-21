package topics

import (
	"fmt"
	"regexp"
	"strings"
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
