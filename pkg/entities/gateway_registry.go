package entities

// GatewayInfo is used when registering with the bootstrap host and when parsing expose-nodes response.
type GatewayInfo struct {
	GatewayID  string `json:"gateway_id"`
	HTTPAddr   string `json:"gateway_http_addr"`
	P2PAddr    string `json:"mump2p_addr"`
	ClusterID  string `json:"cluster_id"`
	ChainID    string `json:"chain_id"`
	Version    string `json:"version"`
	CommitHash string `json:"commit_hash"`
}
