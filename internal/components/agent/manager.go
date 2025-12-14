package agent

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

// Compile-time check that AgentManager implements components.AgentManager
var _ components.AgentManager = (*AgentManager)(nil)
var _ pb.ProbingAgentInterfaceServer = (*AgentManager)(nil)

// AgentManager manages the lifecycle and state of a Probing Agent.
type AgentManager struct {
	pb.UnimplementedProbingAgentInterfaceServer

	mu sync.RWMutex

	currentSpec   *pb.ProbingAgentSpec
	currentStatus *pb.ProbingAgentStatus
}

// NewAgentManager creates a new AgentManager with the given specification.
func NewAgentManager(spec *pb.ProbingAgentSpec) *AgentManager {
	return &AgentManager{
		currentSpec:   spec,
		currentStatus: nil,
	}
}

// Update is called by the orchestrator to push status updates to the generator.
func (m *AgentManager) Update(ctx context.Context, status *pb.ProbingAgentStatus) (*emptypb.Empty, error) {
	m.SetStatus(status)
	log.Printf("received status update from agent: rate=%d", status.ProbingRateCap)
	return &emptypb.Empty{}, nil
}

// GetSpec returns the agent's current specification.
func (m *AgentManager) GetSpec() *pb.ProbingAgentSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentSpec
}

// SetSpec updates the agent's specification.
func (m *AgentManager) SetSpec(spec *pb.ProbingAgentSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentSpec = spec
}

// GetStatus returns the agent's current status.
func (m *AgentManager) GetStatus() *pb.ProbingAgentStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStatus
}

// SetStatus updates the agent's status.
func (m *AgentManager) SetStatus(status *pb.ProbingAgentStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentStatus = status
}

// Run starts the agent's main processing loop.
// It connects to the orchestrator, registers itself, and streams elements.
// It blocks until the context is cancelled or max retries are exceeded.
func (m *AgentManager) Run(ctx context.Context) error {
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
			log.Printf("failed to connect for unregistration: %v", err)
			return
		}
		defer conn.Close()

		_, err = client.UnRegisterComponent(cleanupCtx, &pb.UnRegisterComponentRequest{
			Uuid: status.Uuid,
		})
		if err != nil {
			log.Printf("failed to unregister component: %v", err)
			return
		}

		log.Printf("successfully unregistered from orchestrator, uuid=%s", status.Uuid)
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
			log.Printf("failed to open connection with the orchestrator (num_tries=%d): %v", numRetries, err)
			if conn != nil {
				conn.Close()
			}
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return nil
			}
			continue
		}

		// If the uuid is not given by the orchestrator we need to register the ourselves.
		if m.currentStatus == nil || m.currentStatus.Uuid == "" {
			res, err := client.RegisterComponent(ctx, &pb.RegisterComponentRequest{
				Component: &pb.RegisterComponentRequest_ProbingAgentSpec{
					ProbingAgentSpec: spec,
				},
			})
			if err != nil {
				numRetries++
				log.Printf("failed to register agent with the orchestrator (num_tries=%d): %v", numRetries, err)
				conn.Close()
				if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
					return nil
				}
				continue
			}

			// Validate response
			if res.GetProbingAgentStatus() == nil {
				log.Println("failed to get status from orchestrator, this is likely a bug on the orchestrator side")
				conn.Close()
				continue
			}
			m.SetStatus(res.GetProbingAgentStatus())
			log.Printf("registered with orchestrator, uuid=%s", m.GetStatus().Uuid)
		}

		// Reset retries on successful registration
		numRetries = 0

		// Try to establish a stream
		stream, err := client.PushForwardingInfoElements(ctx)
		if err != nil {
			numRetries++
			log.Printf("failed to open stream with the orchestrator (num_tries=%d): %v", numRetries, err)
			conn.Close()
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return nil
			}
			continue
		}

		// Stream elements until error or context cancellation
		m.streamElements(ctx, stream)

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

// streamElements receives directives and sends back forwarding info elements.
func (m *AgentManager) streamElements(ctx context.Context, stream grpc.BidiStreamingClient[pb.ForwardingInfoElement, pb.ProbingDirective]) {
	log.Println("started streaming elements")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Receive directive from orchestrator
		directive, err := stream.Recv()
		if err != nil {
			log.Printf("error receiving directive: %v", err)
			return
		}

		// Process directive and generate element
		element := m.processDirective(directive)
		if element == nil {
			continue
		}

		// Send element back to orchestrator
		if err := stream.Send(element); err != nil {
			log.Printf("error sending element: %v", err)
			return
		}
	}
}

// processDirective processes a single directive and returns the result.
// Override this method in a subtype for custom probing logic.
func (m *AgentManager) processDirective(directive *pb.ProbingDirective) *pb.ForwardingInfoElement {
	return &pb.ForwardingInfoElement{ // TODO
		VantagePoint:    m.currentSpec.VantagePoint,
		NearTtl:         10,
		SourceAddr:      net.ParseIP("1.1.1.1"),
		DestinationAddr: directive.DestinationAddress,
	}
}

// Register registers the generator with a gRPC server.
func (m *AgentManager) Register(server *grpc.Server) {
	pb.RegisterProbingAgentInterfaceServer(server, m)
}
