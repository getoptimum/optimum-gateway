package test_utils

import (
	commonentities "github.com/getoptimum/optimum-common/pkg/entities"
	cfgpkg "github.com/getoptimum/optimum-gateway/pkg/config"
)

// TestDynamicConfig is the coding row the in-process bootstrap returns.
// The service version is whatever the gateway asked for, so the startup check matches.
func TestDynamicConfig(chainID, clusterID, serviceVersion string) commonentities.DynamicConfig {
	return commonentities.DynamicConfig{
		ChainID:                  chainID,
		ClusterID:                clusterID,
		ServiceVersion:           serviceVersion,
		PropagationEnabled:       true,
		ExcludeSelfMessages:      true,
		RandomMessageSize:        cfgpkg.DefaultRandomMessageSize,
		ShardFactor:              cfgpkg.DefaultShardFactor,
		PublisherShardMultiplier: 1.5,
		ForwardShardThreshold:    cfgpkg.DefaultForwardShardThreshold,
		MeshDegreeMin:            12,
		MeshDegreeTarget:         16,
		MeshDegreeMax:            24,
		MaxShardSize:             4096,
		RLNCK:                    16,
		RedundancyFraction:       1.5,
		RelayCreditMultiplier:    5,
		HeartbeatMS:              250,
	}
}

// TestOptimumConfig is the same row as a mesh config, for a node that reads
// the rotator before a fetch completes.
func TestOptimumConfig() *commonentities.OptimumConfig {
	dc := TestDynamicConfig("hoodi", "test", "gateway-dev")
	return (&commonentities.OptimumConfig{
		MaxMessageSize: cfgpkg.DefaultMaxMessageSize,
	}).ApplyDynamicConfig(&dc)
}
