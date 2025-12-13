package pa

import (
	"context"
	"io"
	"iprl-demo/internal/api"
	"iprl-demo/internal/clients"
	pb "iprl-demo/internal/gen/proto"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type MockAgent struct {
	mu  sync.RWMutex
	cfg *api.MockPAConfig
	rng *rand.Rand

	poClient *clients.POClient

	directiveCh chan *pb.ProbingDirective

	self *pb.ProbingAgent

	doneCtx context.Context
	doneFn  context.CancelFunc
}

var _ PA = (*MockAgent)(nil)

func NewMockAgent(poClient *clients.POClient, cfg *api.MockPAConfig) *MockAgent {
	doneCtx, doneFn := context.WithCancel(context.Background())
	return &MockAgent{
		rng:         rand.New(rand.NewSource(cfg.Seed)),
		cfg:         cfg,
		poClient:    poClient,
		directiveCh: make(chan *pb.ProbingDirective, cfg.DirectiveBufferLength),
		doneCtx:     doneCtx,
		doneFn:      doneFn,

		// we are omitting the unknown fields
		self: &pb.ProbingAgent{
			SoftwareVersion: cfg.SoftwareVersion,
			GrpcAddr:        cfg.Address,
			VantagePoint: &pb.VantagePoint{
				Ip:       cfg.VantagePointIP,
				Id:       cfg.VantagePointID,
				Asn:      cfg.GetVantagePointASN(),
				Location: cfg.GetVantagePointLocation(),
				Provider: cfg.GetVantagePointProvider(),
			},
			Tags:                 map[string]string{},
			Status:               pb.ProbingAgentStatus_PROBING_AGENT_STATUS_ONLINE,
			ProbingRatePerSecond: cfg.DefaultProbingRatePerSecond,
		},
	}
}

func (g *MockAgent) GetSelf() *pb.ProbingAgent {
	return g.self
}

func (g *MockAgent) SetSelf(self *pb.ProbingAgent) {
	log.Printf("g.self: %v\n", g.self)
	g.self = self
}

func (g *MockAgent) SetClusterStatus(cs *pb.ClusterStatus) {
	// update self
	for _, pa := range cs.Agents {
		if pa.Id == g.self.Id {
			g.SetSelf(pa)
			break
		}
	}
}

func (g *MockAgent) DirectiveChannel() chan<- *pb.ProbingDirective {
	return g.directiveCh
}

func (g *MockAgent) Done() <-chan struct{} {
	return g.doneCtx.Done()
}

func (g *MockAgent) Run(ctx context.Context) {
	select {
	case <-g.doneCtx.Done():
		log.Println("failed to run mock agent second time, it can only be run once.")
		return
	default:
		// pass
	}

	defer func() {
		g.doneFn()
		close(g.directiveCh)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			stream, err := g.poClient.EnqueueForwardingInfoElement(ctx)
			if err != nil {
				log.Printf("failed to open stream to PO: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			g.streamElements(ctx, stream)

			if _, err := stream.CloseAndRecv(); err != nil {
				log.Printf("error on closing the stream: %v", err)
			}
		}
	}
}

func (g *MockAgent) streamElements(ctx context.Context, stream grpc.ClientStreamingClient[pb.ForwardingInfoElement, emptypb.Empty]) {
	for {
		// Read rate dynamically
		g.mu.RLock()
		rate := g.self.ProbingRatePerSecond // Currently rate limiting does not work
		g.mu.RUnlock()

		// stop the generation if the rate is set to zero.
		if rate == 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second): // poll every 5 seconds
				continue
			}
		}

		select {
		case <-ctx.Done():
			return
		case directive, ok := <-g.directiveCh:
			if !ok {
				return
			}

			element := g.generateElement(directive)
			if element == nil {
				continue
			}

			if err := stream.Send(element); err != nil {
				if err == io.EOF {
					log.Println("Stream closed by PO")
				} else {
					log.Printf("failed to send directive: %v", err)
				}
				return
			}
		}

	}
}

// This is the core generation logic.
func (g *MockAgent) generateElement(directive *pb.ProbingDirective) *pb.ForwardingInfoElement {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()

	// Simulate RTT (10-100ms)
	rttDuration := time.Duration(10+g.rng.Intn(90)) * time.Millisecond
	sentAt := now.Add(-rttDuration)

	// Generate reply addresses (simulating routers along the path)
	nearReplyAddr := make(net.IP, 4)
	farReplyAddr := make(net.IP, 4)
	for i := range nearReplyAddr {
		nearReplyAddr[i] = byte(g.rng.Intn(256))
		farReplyAddr[i] = byte(g.rng.Intn(256))
	}

	// Near TTL is typically directive TTL - hops traveled
	hopsToNear := 1 + g.rng.Intn(int(directive.Ttl))
	nearTTL := uint32(hopsToNear)
	farTTL := nearTTL + 1
	if farTTL > directive.Ttl {
		farTTL = directive.Ttl
	}

	// Simulate near/far RTT (far is always >= near)
	nearRTTDuration := time.Duration(5+g.rng.Intn(45)) * time.Millisecond
	farRTTDuration := nearRTTDuration + time.Duration(5+g.rng.Intn(20))*time.Millisecond

	element := &pb.ForwardingInfoElement{
		VantagePoint:    g.self.VantagePoint,
		FlowId:          g.rng.Uint64(),
		NearTtl:         nearTTL,
		FarTtl:          farTTL,
		NearReplyAddr:   nearReplyAddr,
		FarReplyAddr:    farReplyAddr,
		DestinationAddr: directive.DstAddr,
		PacketSize:      64 + int32(g.rng.Intn(64)), // 64-128 bytes
		NearRtt: &pb.RTTInfo{
			Duration:   durationpb.New(nearRTTDuration),
			SentAt:     timestamppb.New(sentAt),
			ReceivedAt: timestamppb.New(sentAt.Add(nearRTTDuration)),
		},
		FarRtt: &pb.RTTInfo{
			Duration:   durationpb.New(farRTTDuration),
			SentAt:     timestamppb.New(sentAt),
			ReceivedAt: timestamppb.New(sentAt.Add(farRTTDuration)),
		},
		PacketsSent:     1 + int32(g.rng.Intn(3)),
		PacketsReceived: 1,
		ChangeType:      pb.ChangeType_CHANGE_TYPE_NONE,
		Justification:   pb.JustificationType_JUSTIFICATION_TYPE_PROBE,
		VersionTag:      pb.MethodVersion_METHOD_VERSION_V1,
		Timestamp:       timestamppb.New(now),
	}

	// Occasionally add MPLS info (~20% of responses)
	if g.rng.Float32() < 0.2 {
		element.Mpls = &pb.MPLSInfo{
			Labels: []*pb.MPLSLabel{
				{
					Label: uint32(16 + g.rng.Intn(1048560)), // Valid MPLS label range
					Tc:    uint32(g.rng.Intn(8)),            // 3 bits
					S:     true,                             // Bottom of stack
					Ttl:   uint32(1 + g.rng.Intn(255)),
				},
			},
		}
	}

	// Occasionally add MDA parameters (~30% of responses)
	if g.rng.Float32() < 0.3 {
		element.Mda = &pb.MDAParameters{
			FlowCount:   1 + int32(g.rng.Intn(16)),
			Confidence:  0.95 + g.rng.Float64()*0.05, // 95-100%
			MaxRetries:  3,
			AlgoVersion: "mda-v1",
		}
	}

	return element
}
