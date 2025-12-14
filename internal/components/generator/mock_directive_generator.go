package generator

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"iprl-demo/internal/components"
	pb "iprl-demo/internal/gen/proto"

	"google.golang.org/protobuf/proto"
)

type MockDirectiveGenerator struct {
	outCh chan *pb.ProbingDirective

	rng *rand.Rand

	closed  atomic.Bool
	closeCh chan struct{}
	once    sync.Once

	generatorManager components.GeneratorManager
}

var _ components.DirectiveGenerator = (*MockDirectiveGenerator)(nil)

// NewMockDirectiveGenerator creates a mock directive generator.
// interval controls how often directives are generated.
func NewMockDirectiveGenerator(buffer int, seed int64, generatorManager components.GeneratorManager) (*MockDirectiveGenerator, error) {
	return &MockDirectiveGenerator{
		outCh:            make(chan *pb.ProbingDirective, buffer),
		rng:              rand.New(rand.NewSource(seed)),
		closeCh:          make(chan struct{}),
		generatorManager: generatorManager,
	}, nil
}

// PullChannel implements Puller.
func (m *MockDirectiveGenerator) PullChannel() <-chan *pb.ProbingDirective {
	return m.outCh
}

// Save the reference of the manager to access the spec and status if necessary.
func (m *MockDirectiveGenerator) Register(g components.GeneratorManager) {
	m.generatorManager = g
}

// Run implements Runner.
func (m *MockDirectiveGenerator) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-m.closeCh:
			return nil

		default:
			dir := m.generateDirective()
			if dir == nil {
				continue
			}

			select {
			case m.outCh <- dir:
			case <-ctx.Done():
				return nil
			case <-m.closeCh:
				return nil
			}
		}
	}
}

// Close implements io.Closer.
func (m *MockDirectiveGenerator) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.once.Do(func() {
		close(m.closeCh)
		close(m.outCh)
	})

	return nil
}

func (g *MockDirectiveGenerator) generateDirective() *pb.ProbingDirective {
	spec := g.generatorManager.GetSpec()
	status := g.generatorManager.GetStatus()

	if status == nil || len(status.AgentSpecs) == 0 {
		time.Sleep(time.Second * 5)
		return nil
	}

	// Random vantage point from available agents
	agentSpec := status.AgentSpecs[g.rng.Intn(len(status.AgentSpecs))]

	// Random destination IP
	ip := make(net.IP, 4)
	for i := range ip {
		ip[i] = byte(g.rng.Intn(256))
	}

	// Random TTL in range
	ttl := spec.MinTtl
	if spec.MaxTtl > spec.MinTtl {
		ttl = spec.MinTtl + uint32(g.rng.Intn(int(spec.MaxTtl-spec.MinTtl+1)))
	}

	// Random protocol from configured list
	var protocol pb.Protocol
	if len(spec.Protocols) > 0 {
		protocol = spec.Protocols[g.rng.Intn(len(spec.Protocols))]
	} else {
		protocol = pb.Protocol_PROTOCOL_ICMP
	}

	// Random destination port (for TCP/UDP)
	var dstPort uint32
	if protocol == pb.Protocol_PROTOCOL_TCP || protocol == pb.Protocol_PROTOCOL_UDP {
		dstPort = uint32(1 + g.rng.Intn(65535))
	}

	return &pb.ProbingDirective{
		VantagePointName:   agentSpec.VantagePoint.Name,
		DestinationAddress: ip,
		IpVersion:          pb.IPVersion_IP_VERSION_4,
		Protocol:           protocol,
		NearTtl:            ttl,
		HeaderParameters: &pb.HeaderParameters{
			SourcePort:      proto.Uint32(uint32(30000 + g.rng.Intn(30000))),
			DestinationPort: proto.Uint32(dstPort),
			TypeOfService:   proto.Uint32(0),
			DfFlag:          proto.Bool(false),
		},
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
