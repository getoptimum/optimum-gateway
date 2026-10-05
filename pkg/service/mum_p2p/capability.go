package mum_p2p

// PeerCapability is the mesh admission capability resolved at handshake time.
// It mirrors optimum-pubsub's PeerCapability without depending on optimum-p2p.
type PeerCapability struct {
	CanPublish bool
}
