package agent

import (
	"context"
	"log"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
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

	prober components.Prober
}

// NewAgentManager creates a new AgentManager with the given specification.
func NewAgentManager(spec *pb.ProbingAgentSpec, prober components.Prober) *AgentManager {
	return &AgentManager{
		currentSpec:   spec,
		currentStatus: nil,
		prober:        prober,
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
	m.prober.Register(m)

	// try to unregister before shutdown
	defer func() {
		m.prober.Close()

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

		log.Printf("agent with uuid=%s successfully left the cluster", status.Uuid)
	}()

	g, ctx2 := errgroup.WithContext(ctx)

	g.Go(func() error {
		return m.prober.Run(ctx2)
	})

	g.Go(func() error {
		numRetries := uint32(0)

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
					Spec: &pb.JoinRequest_ProbingAgentSpec{
						ProbingAgentSpec: spec,
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
				if res.GetProbingAgentStatus() == nil {
					log.Println("failed to get a valid status from the orchestrator, this is likely a bug on the orchestrator side")
					conn.Close()
					continue
				}
				m.SetStatus(res.GetProbingAgentStatus())
				log.Printf("agent with uuid=%s successfully joined to the cluster", m.GetStatus().Uuid)
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

			// Stream until error or context cancellation
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
	})

	if err := g.Wait(); err != nil {
		log.Printf("there was an error on the run method: %v", err)
	}

	return nil
}

// streamElements receives directives and sends back forwarding info elements.
func (m *AgentManager) streamElements(ctx context.Context, stream grpc.BidiStreamingClient[pb.ForwardingInfoElement, pb.ProbingDirective]) {
	g, ctx := errgroup.WithContext(ctx)

	// Receive and push to the push channel
	g.Go(func() error {
		defer m.prober.Close()

		for {
			directive, err := stream.Recv()
			if err != nil {
				return err
			}

			select {
			case m.prober.PushChannel() <- directive:
			case <-ctx.Done():
				return nil
			}
		}
	})

	// Pull and send to the orchestrator
	g.Go(func() error {
		for {
			select {
			case element, ok := <-m.prober.PullChannel():
				if !ok {
					return nil
				}

				// Rate limit here (correct place)
				interval := time.Second / time.Duration(m.GetStatus().ProbingRateCap)
				if err := util.SleepWithContext(ctx, interval); err != nil {
					return err
				}

				if err := stream.Send(element); err != nil {
					return err
				}

			case <-ctx.Done():
				return nil
			}
		}
	})

	// Wait for all goroutines
	if err := g.Wait(); err != nil {
		log.Printf("there was an error on the probing method: %v", err)
		return
	}
}

// Register registers the generator with a gRPC server.
func (m *AgentManager) Register(server *grpc.Server) {
	pb.RegisterProbingAgentInterfaceServer(server, m)
}
