package api

import (
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"net"
	"time"
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

type ForwardingInfoElementJSON struct {
	VantagePoint struct {
		Name          string  `json:"name"`
		PublicAddress string  `json:"public_address"`
		ASN           *uint32 `json:"asn,omitempty"`
		Provider      string  `json:"provider"`
	} `json:"vantage_point"`

	FlowID           uint64 `json:"flow_id"`
	NearTTL          uint32 `json:"near_ttl"`
	NearReplyAddress string `json:"near_reply_address"`
	FarReplyAddress  string `json:"far_reply_address"`
	DestinationAddr  string `json:"destination_addr"`
	SourceAddr       string `json:"source_addr,omitempty"`

	PacketSize      int32  `json:"packet_size"`
	PacketsSent     uint32 `json:"packets_sent"`
	PacketsReceived uint32 `json:"packets_received"`

	NearRTT *RTTInfoJSON `json:"near_rtt,omitempty"`
	FarRTT  *RTTInfoJSON `json:"far_rtt,omitempty"`

	MPLS *MPLSInfoJSON `json:"mpls,omitempty"`
	MDA  *MDAJSON      `json:"mda,omitempty"`

	ChangeType        string    `json:"change_type"`
	JustificationType string    `json:"justification_type"`
	ConstructionTime  time.Time `json:"construction_timestamp"`
}

type RTTInfoJSON struct {
	SentAt     time.Time `json:"sent_at"`
	ReceivedAt time.Time `json:"received_at"`
}

type MPLSInfoJSON struct {
	Labels []MPLSLabelJSON `json:"labels"`
}

type MPLSLabelJSON struct {
	Label uint32 `json:"label"`
	TC    uint32 `json:"tc"`
	S     bool   `json:"s"`
	TTL   uint32 `json:"ttl"`
}

type MDAJSON struct {
	FlowCount   int32   `json:"flow_count"`
	Confidence  float64 `json:"confidence"`
	MaxRetries  int32   `json:"max_retries"`
	AlgoVersion string  `json:"algo_version"`
}

func FromProto(elem *pb.ForwardingInfoElement) *ForwardingInfoElementJSON {
	if elem == nil {
		return nil
	}

	out := &ForwardingInfoElementJSON{}

	// --- Vantage point ---
	if vp := elem.VantagePoint; vp != nil {
		out.VantagePoint.Name = vp.Name
		out.VantagePoint.PublicAddress = util.IpBytesToString(vp.PublicAddress)
		out.VantagePoint.Provider = vp.Provider.String()

		if vp.Asn != nil {
			asn := *vp.Asn
			out.VantagePoint.ASN = &asn
		}
	}

	// --- Core fields ---
	out.FlowID = elem.FlowId
	out.NearTTL = elem.NearTtl
	out.NearReplyAddress = util.IpBytesToString(elem.NearReplyAddress)
	out.FarReplyAddress = util.IpBytesToString(elem.FarReplyAddress)
	out.DestinationAddr = util.IpBytesToString(elem.DestinationAddr)
	out.SourceAddr = util.IpBytesToString(elem.SourceAddr)

	out.PacketSize = elem.PacketSize
	out.PacketsSent = elem.PacketsSent
	out.PacketsReceived = elem.PacketsReceived

	// --- RTT ---
	if elem.NearRtt != nil {
		out.NearRTT = &RTTInfoJSON{
			SentAt:     util.TSToTime(elem.NearRtt.SentAt),
			ReceivedAt: util.TSToTime(elem.NearRtt.ReceivedAt),
		}
	}
	if elem.FarRtt != nil {
		out.FarRTT = &RTTInfoJSON{
			SentAt:     util.TSToTime(elem.FarRtt.SentAt),
			ReceivedAt: util.TSToTime(elem.FarRtt.ReceivedAt),
		}
	}

	// --- MPLS ---
	if elem.Mpls != nil {
		mpls := &MPLSInfoJSON{}
		for _, l := range elem.Mpls.Labels {
			mpls.Labels = append(mpls.Labels, MPLSLabelJSON{
				Label: l.Label,
				TC:    l.Tc,
				S:     l.S,
				TTL:   l.Ttl,
			})
		}
		out.MPLS = mpls
	}

	// --- MDA ---
	if elem.Mda != nil {
		out.MDA = &MDAJSON{
			FlowCount:   elem.Mda.FlowCount,
			Confidence:  elem.Mda.Confidence,
			MaxRetries:  elem.Mda.MaxRetries,
			AlgoVersion: elem.Mda.AlgoVersion,
		}
	}

	out.ChangeType = elem.ChangeType.String()
	out.JustificationType = elem.JustificationType.String()
	out.ConstructionTime = util.TSToTime(elem.ConstructionTimestamp)

	return out
}
