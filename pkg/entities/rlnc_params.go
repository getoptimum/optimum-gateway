package entities

// Where a reported set of RLNC parameters came from. The startup log and
// /api/v1/self_info use the same words, so the two can be compared directly
// instead of an operator having to work out which of them is authoritative.
const (
	// RLNCParamsSourceNode marks parameters read back from the running node.
	RLNCParamsSourceNode = "node"
	// RLNCParamsSourceDynamicConfig marks the dynamic config's current view,
	// reported only while there is no node to ask. It is what the config says,
	// not what any mesh is doing.
	RLNCParamsSourceDynamicConfig = "dynamic_config"
)

// RLNCParams are the coding and mesh values a running mump2p node resolved when
// it was built: what its coder shards at, and what its router forwards and fans
// out on.
//
// They are a snapshot taken at construction, not the dynamic config's current
// view. The two diverge whenever a served value lands after the node is wired,
// so reporting the served view as if it were the node's is how an operator ends
// up reading a generation size the node never coded at.
type RLNCParams struct {
	// ShardFactor is k, the generation size the coder shards at.
	ShardFactor uint32
	// MaxShardSize caps coefficients plus data per shard, in bytes.
	MaxShardSize uint32
	// RedundancyFraction multiplies k to give the coded symbol count.
	RedundancyFraction float64
	// ForwardThreshold is the fraction of k a node's rank must pass to forward.
	ForwardThreshold float64
	// ForwardRankThreshold is int(k * ForwardThreshold): the rank the router
	// must strictly exceed before it recodes and forwards. Derived here so the
	// reported gate is the arithmetic the router runs, not a restatement of it.
	ForwardRankThreshold int
	MeshDegreeTarget     int
	MeshDegreeMin        int
	// MeshDegreeMax is the Dhi budget the router caps threshold-forward fanout to.
	MeshDegreeMax int
}
