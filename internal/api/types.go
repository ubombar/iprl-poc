package api

import (
	pb "iprl-demo/internal/gen/proto"
	"net"
)

// This is the config struct of mock implementation of the PDG
type MockPDGConfig struct {
	SoftwareVersion              string `json:"software_version"`
	DefaultProbingRatePerSeconds uint   `json:"default_probing_rate_per_seconds"`
	Seed                         int64  `json:"seed"`
	MinTTL                       uint8  `json:"min_ttl"`
	MaxTTL                       uint8  `json:"max_ttl"`
	Address                      string `json:"address"`
}

// This is the config struct of mock implementation of the PA
type MockPAConfig struct {
	SoftwareVersion             string `json:"software_version"`
	Seed                        int64  `json:"seed"`
	Address                     string `json:"address"`
	DefaultProbingRatePerSecond uint32 `json:"default_probing_rate_per_second"`
	DirectiveBufferLength       uint   `json:"directive_buffer_length"`

	// VP stuff
	VantagePointIP       net.IP `json:"vantage_point_ip"`
	VantagePointID       string `json:"vantage_point_id"`
	VantagePointASN      uint32 `json:"vantage_point_asn"`
	VantagePointLocation string `json:"vantage_point_location"`
	VantagePointProvider string `json:"vantage_point_provider"`
}

func (m MockPAConfig) GetVantagePointASN() *uint32 {
	if m.VantagePointASN == 0 {
		return nil
	}
	return &m.VantagePointASN
}

func (m MockPAConfig) GetVantagePointLocation() *string {
	if m.VantagePointLocation == "" {
		return nil
	}
	return &m.VantagePointLocation
}

func (m MockPAConfig) GetVantagePointProvider() pb.VantagePointProvider {
	switch m.VantagePointProvider {
	case "gcp":
		return pb.VantagePointProvider_VANTAGE_POINT_PROVIDER_GCP
	case "aws":
		return pb.VantagePointProvider_VANTAGE_POINT_PROVIDER_AWS
	case "edgenet":
		return pb.VantagePointProvider_VANTAGE_POINT_PROVIDER_EDGENET
	default:
		return pb.VantagePointProvider_VANTAGE_POINT_PROVIDER_UNKNOWN
	}
}

// This is the config struct of mock implementation of the PDG
type POConfig struct {
	SoftwareVersion              string `json:"software_version"`
	DefaultProbingRatePerSeconds uint   `json:"default_probing_rate_per_seconds"`
	Address                      string `json:"address"`
}
