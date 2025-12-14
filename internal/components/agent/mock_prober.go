package agent

import (
	"context"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"iprl-demo/internal/components"
	pb "iprl-demo/internal/gen/proto"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type MockProber struct {
	inCh  chan *pb.ProbingDirective
	outCh chan *pb.ForwardingInfoElement

	closed  atomic.Bool
	closeCh chan struct{}
	rng     *rand.Rand

	agentManager components.AgentManager

	once sync.Once
}

var _ components.Prober = (*MockProber)(nil)

// NewMockProber creates a mock prober with buffered channels.
func NewMockProber(buffer int, seed int64, agentManager components.AgentManager) (*MockProber, error) {
	return &MockProber{
		inCh:         make(chan *pb.ProbingDirective, buffer),
		outCh:        make(chan *pb.ForwardingInfoElement, buffer),
		closeCh:      make(chan struct{}),
		rng:          rand.New(rand.NewSource(seed)),
		agentManager: agentManager,
	}, nil
}

// PushChannel implements Pusher.
func (m *MockProber) PushChannel() chan<- *pb.ProbingDirective {
	return m.inCh
}

// PullChannel implements Puller.
func (m *MockProber) PullChannel() <-chan *pb.ForwardingInfoElement {
	return m.outCh
}

// Save the reference of the manager to access the spec and status if necessary.
func (m *MockProber) Register(g components.AgentManager) {
	m.agentManager = g
}

// Run implements Runner.
func (m *MockProber) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		case <-m.closeCh:
			return nil

		case dir, ok := <-m.inCh:
			if !ok {
				// Input channel should not be closed by callers,
				// but we handle it defensively.
				return nil
			}

			// Simulate some work
			time.Sleep(10 * time.Millisecond)

			elem := m.generateElement(dir)
			if elem == nil {
				continue
			}

			select {
			case m.outCh <- elem:
			case <-ctx.Done():
				return nil
			case <-m.closeCh:
				return nil
			}
		}
	}
}

// Close implements io.Closer.
func (m *MockProber) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.once.Do(func() {
		close(m.closeCh)
		close(m.outCh)
	})

	return nil
}

// This is the core generation logic.
func (m *MockProber) generateElement(directive *pb.ProbingDirective) *pb.ForwardingInfoElement {
	spec := m.agentManager.GetSpec()
	now := time.Now()

	// Simulate RTT (10-100ms)
	rttDuration := time.Duration(10+m.rng.Intn(90)) * time.Millisecond
	sentAt := now.Add(-rttDuration)

	// Generate reply addresses (simulating routers along the path)
	nearReplyAddr := make(net.IP, 4)
	farReplyAddr := make(net.IP, 4)
	for i := range nearReplyAddr {
		nearReplyAddr[i] = byte(m.rng.Intn(256))
		farReplyAddr[i] = byte(m.rng.Intn(256))
	}

	// Near TTL is typically directive TTL - hops traveled
	hopsToNear := 1 + m.rng.Intn(int(directive.NearTtl))
	nearTTL := uint32(hopsToNear)
	farTTL := nearTTL + 1
	if farTTL > directive.NearTtl {
		farTTL = directive.NearTtl
	}

	// Simulate near/far RTT (far is always >= near)
	nearRTTDuration := time.Duration(5+m.rng.Intn(45)) * time.Millisecond
	farRTTDuration := nearRTTDuration + time.Duration(5+m.rng.Intn(20))*time.Millisecond

	element := &pb.ForwardingInfoElement{
		VantagePoint:     spec.VantagePoint,
		FlowId:           m.rng.Uint64(),
		NearTtl:          nearTTL,
		NearReplyAddress: nearReplyAddr,
		FarReplyAddress:  farReplyAddr,
		DestinationAddr:  directive.DestinationAddress,
		PacketSize:       64 + int32(m.rng.Intn(64)), // 64-128 bytes
		NearRtt: &pb.RTTInfo{
			SentAt:     timestamppb.New(sentAt),
			ReceivedAt: timestamppb.New(sentAt.Add(nearRTTDuration)),
		},
		FarRtt: &pb.RTTInfo{
			SentAt:     timestamppb.New(sentAt),
			ReceivedAt: timestamppb.New(sentAt.Add(farRTTDuration)),
		},
		PacketsSent:           1 + uint32(m.rng.Intn(3)),
		PacketsReceived:       1,
		ChangeType:            pb.ChangeType_ANNOUNCED,
		JustificationType:     pb.JustificationType_OBSERVED,
		ConstructionTimestamp: timestamppb.New(now),
	}

	// Occasionally add MPLS info (~20% of responses)
	if m.rng.Float32() < 0.2 {
		element.Mpls = &pb.MPLSInfo{
			Labels: []*pb.MPLSLabel{
				{
					Label: uint32(16 + m.rng.Intn(1048560)), // Valid MPLS label range
					Tc:    uint32(m.rng.Intn(8)),            // 3 bits
					S:     true,                             // Bottom of stack
					Ttl:   uint32(1 + m.rng.Intn(255)),
				},
			},
		}
	}

	// Occasionally add MDA parameters (~30% of responses)
	if m.rng.Float32() < 0.3 {
		element.Mda = &pb.MDAParameters{
			FlowCount:   1 + int32(m.rng.Intn(16)),
			Confidence:  0.95 + m.rng.Float64()*0.05, // 95-100%
			MaxRetries:  3,
			AlgoVersion: "mda-v1",
		}
	}

	return element
}
