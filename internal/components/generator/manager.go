package generator

import (
	"context"
	"log"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"iprl-demo/internal/clients"
	"iprl-demo/internal/components"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
)

// Compile-time check that GeneratorManager implements components.GeneratorManager
var _ components.GeneratorManager = (*GeneratorManager)(nil)
var _ pb.ProbingDirectiveGeneratorInterfaceServer = (*GeneratorManager)(nil)

// GeneratorManager manages the lifecycle and state of a Probing Directive Generator.
type GeneratorManager struct {
	pb.UnimplementedProbingDirectiveGeneratorInterfaceServer

	mu sync.RWMutex

	currentSpec   *pb.ProbingDirectiveGeneratorSpec
	currentStatus *pb.ProbingDirectiveGeneratorStatus
}

// NewGeneratorManager creates a new GeneratorManager with the given specification.
func NewGeneratorManager(spec *pb.ProbingDirectiveGeneratorSpec) *GeneratorManager {
	return &GeneratorManager{
		currentSpec:   spec,
		currentStatus: nil,
	}
}

// Update is called by the orchestrator to push status updates to the generator.
func (m *GeneratorManager) Update(ctx context.Context, status *pb.ProbingDirectiveGeneratorStatus) (*emptypb.Empty, error) {
	m.SetStatus(status)
	log.Printf("received status update from generator: rate=%d num_agents=%d",
		status.ProbeGenerationRatePerSecondPerAgent,
		len(status.AgentSpecs))
	return &emptypb.Empty{}, nil
}

// GetSpec returns the generator's current specification.
func (m *GeneratorManager) GetSpec() *pb.ProbingDirectiveGeneratorSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentSpec
}

// SetSpec updates the generator's specification.
func (m *GeneratorManager) SetSpec(spec *pb.ProbingDirectiveGeneratorSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentSpec = spec
}

// GetStatus returns the generator's current status.
func (m *GeneratorManager) GetStatus() *pb.ProbingDirectiveGeneratorStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStatus
}

// SetStatus updates the generator's status.
func (m *GeneratorManager) SetStatus(status *pb.ProbingDirectiveGeneratorStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentStatus = status
}

// Run starts the generator's main processing loop.
// It connects to the orchestrator, registers itself, and streams directives.
// It blocks until the context is cancelled or max retries are exceeded.
func (m *GeneratorManager) Run(ctx context.Context) error {
	numRetries := uint32(0)

	// try to unregister before shutdown
	defer func() {
		status := m.GetStatus()
		if status == nil {
			return
		}

		// Create a new context for cleanup since the original may be cancelled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		client, conn, err := clients.NewInsecureOrchestratorClient(m.GetSpec().OrchestratorAddress)
		if err != nil {
			log.Printf("failed to connect to orchestrator: %v", err)
			return
		}
		defer conn.Close()

		_, err = client.Leave(cleanupCtx, &pb.LeaveRequest{
			Uuid: status.Uuid,
		})
		if err != nil {
			log.Printf("failed to leave the cluster: %v", err)
			return
		}

		log.Printf("generator with uuid=%s successfully left the cluster", status.Uuid)
	}()

	for {
		// Check if max retries exceeded
		spec := m.GetSpec()
		if spec.NumRetries != 0 && numRetries > spec.NumRetries {
			log.Println("max number of retries exceeded, exiting")
			return nil
		}

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Try to connect to the orchestrator
		client, conn, err := clients.NewInsecureOrchestratorClient(spec.OrchestratorAddress)
		if err != nil {
			numRetries++
			log.Printf("failed to connect to the orchestrator (num_tries=%d): %v", numRetries, err)
			if conn != nil {
				conn.Close()
			}
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return nil
			}
			continue
		}

		// If the uuid is not given by the orchestrator we need to register the ourselves.
		if m.currentStatus == nil {
			res, err := client.Join(ctx, &pb.JoinRequest{
				Spec: &pb.JoinRequest_ProbingDirectiveGeneratorSpec{
					ProbingDirectiveGeneratorSpec: spec,
				},
			})
			if err != nil {
				numRetries++
				log.Printf("failed to join to the cluster (num_tries=%d): %v", numRetries, err)
				conn.Close()
				if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
					return nil
				}
				continue
			}

			// Validate response
			if res.GetProbingDirectiveGeneratorStatus() == nil {
				log.Println("failed to get a valid status from the orchestrator, this is likely a bug on the orchestrator side")
				conn.Close()
				continue
			}
			m.SetStatus(res.GetProbingDirectiveGeneratorStatus())
			log.Printf("generator with uuid=%s successfully joined to the cluster", m.GetStatus().Uuid)
		}

		// Reset retries on successful registration
		numRetries = 0

		// Try to establish a stream
		stream, err := client.PushProbingDirectives(ctx)
		if err != nil {
			numRetries++
			log.Printf("failed to open stream with the orchestrator (num_tries=%d): %v", numRetries, err)
			conn.Close()
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return nil
			}
			continue
		}

		// Stream directives until error or context cancellation
		m.streamDirectives(ctx, stream)

		// Close the stream
		if err := stream.CloseSend(); err != nil {
			numRetries++
			log.Printf("failed to close stream with the orchestrator (num_tries=%d): %v", numRetries, err)
		}

		conn.Close()

		// Sleep before reconnecting
		if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
			return nil
		}
	}
}

// streamDirectives generates and sends directives to the orchestrator.
//
// We are doing this in the same Go routine. This is for simplicity.
// Normally this should be in a separate Go routine and run concurrently.
func (m *GeneratorManager) streamDirectives(ctx context.Context, stream grpc.ClientStreamingClient[pb.ProbingDirective, emptypb.Empty]) {

	for {
		// Check context
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Get current rate from status
		status := m.GetStatus()
		if status == nil || status.ProbeGenerationRatePerSecondPerAgent == 0 {
			if err := util.SleepWithContext(ctx, 1*time.Second); err != nil {
				return
			}
			continue
		}

		// Generate directive
		directive := m.generateDirective()
		if directive == nil {
			continue
		}

		// Send directive to orchestrator
		if err := stream.Send(directive); err != nil {
			log.Printf("error sending directive: %v", err)
			return
		}

		// Rate limit
		interval := time.Second / time.Duration(int(status.ProbeGenerationRatePerSecondPerAgent)*len(m.GetStatus().AgentSpecs))

		if err := util.SleepWithContext(ctx, interval); err != nil {
			return
		}
	}
}

// generateDirective generates a single probing directive.
// Override this method in a subtype for custom generation logic.
func (m *GeneratorManager) generateDirective() *pb.ProbingDirective {
	return &pb.ProbingDirective{
		VantagePointName:   "hello bro",
		DestinationAddress: net.ParseIP("1.2.3.4"),
		IpVersion:          pb.IPVersion_IP_VERSION_4,
		Protocol:           pb.Protocol_PROTOCOL_ICMP,
		NearTtl:            4,
		DestinationPort:    0,
	}
}

// Register registers this server with a gRPC server.
func (m *GeneratorManager) Register(server *grpc.Server) {
	pb.RegisterProbingDirectiveGeneratorInterfaceServer(server, m)
}
