package agent

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
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

	mu   sync.RWMutex
	once sync.Once
	ctx  context.Context

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
func (m *AgentManager) Run(parent context.Context) error {
	// This is for adding the reference of manager to this object.
	m.prober.Register(m)

	g, ctx := errgroup.WithContext(parent)
	m.ctx = ctx

	g.Go(func() error {
		return m.runProber(ctx)
	})

	g.Go(func() error {
		return m.runElementStream(ctx)
	})

	g.Go(func() error {
		return m.runRPCServer(ctx)
	})

	g.Go(func() error {
		<-ctx.Done()

		m.leaveCluster()

		m.Close()
		return nil
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

func (m *AgentManager) leaveCluster() {
	status := m.GetStatus()
	if status == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := m.GetSpec()
	if spec == nil {
		return
	}

	client, conn, err := clients.NewInsecureOrchestratorClient(spec.OrchestratorAddress)
	if err != nil {
		log.Printf("leave: failed to connect: %v", err)
		return
	}
	defer conn.Close()

	_, err = client.Leave(ctx, &pb.LeaveRequest{
		Uuid: status.Uuid,
	})
	if err != nil {
		log.Printf("leave: failed: %v", err)
		return
	}

	log.Printf("agent %s left the cluster", status.Uuid)
}

func (m *AgentManager) runProber(ctx context.Context) error {
	return m.prober.Run(ctx)
}

func (m *AgentManager) runRPCServer(ctx context.Context) error {
	grpcServer := grpc.NewServer()
	m.Register(grpcServer)

	lis, err := net.Listen("tcp", m.currentSpec.GrpcAddress)
	if err != nil {
		return err
	}

	log.Printf("grpc server listening on %s", m.currentSpec.GrpcAddress)

	// Shutdown logic
	go func() {
		<-ctx.Done()
		log.Println("grpc shutdown initiated")

		done := make(chan struct{})

		go func() {
			grpcServer.GracefulStop()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
			grpcServer.Stop()
		}

	}()

	// Blocks until Stop / GracefulStop
	err = grpcServer.Serve(lis)
	if err != nil {
		return err
	}

	return nil
}

func (m *AgentManager) runElementStream(ctx context.Context) error {
	bo := backoff.NewExponentialBackOff()

	bo.InitialInterval = 1 * time.Second
	bo.MaxInterval = 5 * time.Second
	bo.MaxElapsedTime = 0 // retry forever

	// Tie backoff lifecycle to context
	boCtx := backoff.WithContext(bo, ctx)

	return backoff.Retry(func() error {
		// Exit cleanly on shutdown
		if err := ctx.Err(); err != nil {
			return backoff.Permanent(err)
		}

		spec := m.GetSpec()

		if spec == nil {
			return errors.New("spec nil")
		}

		client, conn, err := clients.NewInsecureOrchestratorClient(spec.OrchestratorAddress)
		if err != nil {
			return err // retry
		}
		defer conn.Close()

		if err := m.joinCluster(ctx, client, spec); err != nil {
			return err // retry
		}

		stream, err := m.openDirectiveStream(ctx, client)
		if err != nil {
			return err // retry
		}

		if err := m.streamElements(ctx, stream); err != nil {
			return err // retry
		}

		return nil
	}, boCtx)
}

func (m *AgentManager) joinCluster(ctx context.Context, client pb.ProbingOrchestratorInterfaceClient, spec *pb.ProbingAgentSpec) error {
	res, err := client.Join(ctx, &pb.JoinRequest{
		Spec: &pb.JoinRequest_ProbingAgentSpec{
			ProbingAgentSpec: spec,
		},
	})
	if err != nil {
		return err
	}

	status := res.GetProbingAgentStatus()
	if status == nil {
		return errors.New("orchestrator returned nil generator status")
	}

	m.SetStatus(status)
	log.Printf("joined orchestrator cluster as generator %s", status.Uuid)
	return nil
}

func (m *AgentManager) openDirectiveStream(ctx context.Context, client pb.ProbingOrchestratorInterfaceClient) (pb.ProbingOrchestratorInterface_PushForwardingInfoElementsClient, error) {
	return client.PushForwardingInfoElements(ctx)
}

// streamElements receives directives and sends back forwarding info elements.
func (m *AgentManager) streamElements(ctx context.Context, stream grpc.BidiStreamingClient[pb.ForwardingInfoElement, pb.ProbingDirective]) error {
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
		return err
	}

	return nil
}

// Register registers the generator with a gRPC server.
func (m *AgentManager) Register(server *grpc.Server) {
	pb.RegisterProbingAgentInterfaceServer(server, m)
}

func (m *AgentManager) Close() error {
	return nil
}
