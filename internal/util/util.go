package util

import (
	"context"
	pb "iprl-demo/internal/gen/proto"
	"strings"
	"time"
)

func SleepWithContext(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func ParseProtocols(s string) []pb.Protocol {
	if s == "" {
		return []pb.Protocol{pb.Protocol_PROTOCOL_ICMP}
	}

	var protocols []pb.Protocol
	for _, p := range strings.Split(s, ",") {
		switch strings.ToLower(strings.TrimSpace(p)) {
		case "icmp":
			protocols = append(protocols, pb.Protocol_PROTOCOL_ICMP)
		case "tcp":
			protocols = append(protocols, pb.Protocol_PROTOCOL_TCP)
		case "udp":
			protocols = append(protocols, pb.Protocol_PROTOCOL_UDP)
		case "dccp":
			protocols = append(protocols, pb.Protocol_PROTOCOL_DCCP)
		case "icmpv6":
			protocols = append(protocols, pb.Protocol_PROTOCOL_ICMPV6)
		}
	}

	if len(protocols) == 0 {
		return []pb.Protocol{pb.Protocol_PROTOCOL_ICMP}
	}

	return protocols
}

func ParseProvider(s string) pb.VantagePointProvider {
	switch strings.ToLower(s) {
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
