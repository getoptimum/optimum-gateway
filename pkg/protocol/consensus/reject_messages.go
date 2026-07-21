package consensus

// GoodbyeCodeMessages defines a mapping between goodbye codes and string messages.
var GoodbyeCodeMessages = map[uint64]string{
	4:   "client shutdown",
	5:   "irrelevant network",
	6:   "fault/error",
	128: "unable to verify network",
	129: "client has too many peers",
	250: "peer score too low",
	251: "client banned this node",
}
