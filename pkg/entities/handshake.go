package entities

import "fmt"

const (
	HandshakeV1 = "1"
)

type PeerState string

const (
	PeerStateHandshakeValid   PeerState = "handshake_valid"
	PeerStateHandshakeInvalid PeerState = "handshake_invalid"
)

type Handshake struct {
	ClusterID string `json:"cluster_id"`
}

func NewHandshake(clusterID string) *Handshake {
	return &Handshake{
		ClusterID: clusterID,
	}
}

func (h *Handshake) Validate(clusterID string) error {
	if h.ClusterID == "" {
		return fmt.Errorf("handshake has no cluster ID")
	}
	if h.ClusterID != clusterID {
		return fmt.Errorf("handshake cluster ID does not match expected")
	}
	return nil
}

func GetHandshakeTopic(version string) string {
	return fmt.Sprintf("/optimum/v%s/handshake", version)
}
