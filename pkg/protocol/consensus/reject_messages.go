package consensus

// GoodbyeCodeMessages maps goodbye codes to messages: 1-3 from the phase0 spec, the rest client-defined.
var GoodbyeCodeMessages = map[uint64]string{
	1:   "client shutdown",
	2:   "irrelevant network",
	3:   "fault/error",
	128: "unable to verify network",
	129: "client has too many peers",
	130: "rate limited (teku)",
	237: "peer score too low (nimbus)",
	238: "communication timeout (nimbus)",
	250: "peer score too low",
	251: "client banned this node",
	252: "client banned this IP",
}
