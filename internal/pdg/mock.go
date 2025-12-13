package pdg

import (
	"context"
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"iprl-demo/internal/api"
	"iprl-demo/internal/clients"
	pb "iprl-demo/internal/gen/proto"
)

type MockGenerator struct {
	mu  sync.RWMutex
	cfg *api.MockPDGConfig
	rng *rand.Rand

	poClient *clients.POClient
	agents   []*pb.ProbingAgent

	self *pb.ProbingDirectiveGenerator

	doneCtx context.Context
	doneFn  context.CancelFunc
}

func NewMockGenerator(poClient *clients.POClient, cfg *api.MockPDGConfig) *MockGenerator {
	doneCtx, doneFn := context.WithCancel(context.Background())

	return &MockGenerator{
		rng:      rand.New(rand.NewSource(cfg.Seed)),
		cfg:      cfg,
		agents:   make([]*pb.ProbingAgent, 0, 10),
		poClient: poClient,
		doneCtx:  doneCtx,
		doneFn:   doneFn,

		// we are omitting the unknown fields
		self: &pb.ProbingDirectiveGenerator{
			SoftwareVersion:              cfg.SoftwareVersion,
			GrpcAddr:                     cfg.Address,
			ProbeGenerationRatePerSecond: uint32(cfg.DefaultProbingRatePerSeconds),
			MinTtl:                       uint32(cfg.MinTTL),
			MaxTtl:                       uint32(cfg.MaxTTL),
			Tags:                         map[string]string{},
			Settings:                     make(map[string]string),
		},
	}
}

func (g *MockGenerator) GetSelf() *pb.ProbingDirectiveGenerator {
	return g.self
}

func (g *MockGenerator) SetSelf(self *pb.ProbingDirectiveGenerator) {
	log.Printf("g.self: %v\n", g.self)
	g.self = self
}

func (g *MockGenerator) SetClusterStatus(cs *pb.ClusterStatus) {
	// get the probing agents
	g.agents = cs.Agents

	// update self
	for _, pdg := range cs.Generators {
		if pdg.Id == g.self.Id {
			g.SetSelf(pdg)
			break
		}
	}
}

func (g *MockGenerator) Done() <-chan struct{} {
	return g.doneCtx.Done()
}

func (g *MockGenerator) Run(ctx context.Context) {
	select {
	case <-g.doneCtx.Done():
		log.Println("failed to run mock generator second time, it can only be run once.")
		return
	default:
		// pass
	}

	defer func() {
		g.doneFn()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			stream, err := g.poClient.EnqueueDirective(ctx)
			if err != nil {
				log.Printf("failed to open stream to PO: %v", err)
				time.Sleep(5 * time.Second)
				continue
			}

			g.streamDirectives(ctx, stream)

			if _, err := stream.CloseAndRecv(); err != nil {
				log.Printf("error on closing the stream: %v", err)
			}
		}
	}
}

func (g *MockGenerator) streamDirectives(ctx context.Context, stream pb.POService_EnqueueDirectiveClient) {
	for {
		// Read rate dynamically
		g.mu.RLock()
		rate := g.self.ProbeGenerationRatePerSecond
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

		interval := time.Second / time.Duration(rate)

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			directive := g.generateDirective()
			if directive == nil {
				continue
			}
			if err := stream.Send(directive); err != nil {
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

func (g *MockGenerator) generateDirective() *pb.ProbingDirective {
	g.mu.RLock()
	defer g.mu.RUnlock()

	// Need at least one vantage point
	if len(g.self.VantagePointIds) == 0 {
		return nil
	}

	// Random destination IP
	ip := make(net.IP, 4)
	for i := range ip {
		ip[i] = byte(g.rng.Intn(256))
	}

	// Random TTL in range
	ttl := g.self.MinTtl
	if g.self.MaxTtl > g.self.MinTtl {
		ttl = g.self.MinTtl + uint32(g.rng.Intn(int(g.self.MaxTtl-g.self.MinTtl+1)))
	}

	// Random protocol from configured list
	var protocol pb.Protocol
	if len(g.self.Protocols) > 0 {
		protocol = g.self.Protocols[g.rng.Intn(len(g.self.Protocols))]
	}

	// Random vantage point
	vpID := g.self.VantagePointIds[g.rng.Intn(len(g.self.VantagePointIds))]

	return &pb.ProbingDirective{
		Id:             generateID(g.rng),
		VantagePointId: vpID,
		DstAddr:        ip,
		Protocol:       protocol,
		Ttl:            ttl,
		DstPort:        uint32(g.rng.Intn(65535)),
	}
}

func generateID(rng *rand.Rand) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	for i := range b {
		b[i] = chars[rng.Intn(len(chars))]
	}
	return string(b)
}
