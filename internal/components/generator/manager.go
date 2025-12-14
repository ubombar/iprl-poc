package generator

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"iprl-demo/internal/clients"
	"iprl-demo/internal/components"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"

	"github.com/cenkalti/backoff/v4"
)

// Compile-time check that GeneratorManager implements components.GeneratorManager
var _ components.GeneratorManager = (*GeneratorManager)(nil)
var _ pb.ProbingDirectiveGeneratorInterfaceServer = (*GeneratorManager)(nil)

// GeneratorManager manages the lifecycle and state of a Probing Directive Generator.
type GeneratorManager struct {
	pb.UnimplementedProbingDirectiveGeneratorInterfaceServer

	mu   sync.RWMutex
	once sync.Once
	ctx  context.Context

	currentSpec   *pb.ProbingDirectiveGeneratorSpec
	currentStatus *pb.ProbingDirectiveGeneratorStatus

	directiveGenerator components.DirectiveGenerator
}

// NewGeneratorManager creates a new GeneratorManager with the given specification.
func NewGeneratorManager(spec *pb.ProbingDirectiveGeneratorSpec, directiveGenerator components.DirectiveGenerator) *GeneratorManager {
	return &GeneratorManager{
		currentSpec:        spec,
		currentStatus:      nil,
		directiveGenerator: directiveGenerator,
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
func (m *GeneratorManager) Run(parent context.Context) error {
	// This is for adding the reference of manager to this object.
	m.directiveGenerator.Register(m)

	g, ctx := errgroup.WithContext(parent)
	m.ctx = ctx

	g.Go(func() error {
		return m.runDirectiveGenerator(ctx)
	})

	g.Go(func() error {
		return m.runDirectiveStream(ctx)
	})

	g.Go(func() error {
		return m.runRPCServer(ctx)
	})

	g.Go(func() error {
		<-ctx.Done()
		m.Close()
		return nil
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

func (m *GeneratorManager) runDirectiveGenerator(ctx context.Context) error {
	return m.directiveGenerator.Run(ctx)
}

func (m *GeneratorManager) runRPCServer(ctx context.Context) error {
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

func (m *GeneratorManager) runDirectiveStream(ctx context.Context) error {
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

		if err := m.streamDirectives(ctx, stream); err != nil {
			return err // retry
		}

		return nil
	}, boCtx)
}

func (m *GeneratorManager) joinCluster(ctx context.Context, client pb.ProbingOrchestratorInterfaceClient, spec *pb.ProbingDirectiveGeneratorSpec) error {
	res, err := client.Join(ctx, &pb.JoinRequest{
		Spec: &pb.JoinRequest_ProbingDirectiveGeneratorSpec{
			ProbingDirectiveGeneratorSpec: spec,
		},
	})
	if err != nil {
		return err
	}

	status := res.GetProbingDirectiveGeneratorStatus()
	if status == nil {
		return errors.New("orchestrator returned nil generator status")
	}

	m.SetStatus(status)
	log.Printf("joined orchestrator cluster as generator %s", status.Uuid)
	return nil
}

func (m *GeneratorManager) openDirectiveStream(ctx context.Context, client pb.ProbingOrchestratorInterfaceClient) (pb.ProbingOrchestratorInterface_PushProbingDirectivesClient, error) {
	return client.PushProbingDirectives(ctx)
}

// streamDirectives generates and sends directives to the orchestrator.
func (m *GeneratorManager) streamDirectives(ctx context.Context, stream grpc.ClientStreamingClient[pb.ProbingDirective, emptypb.Empty]) error {
	for {
		status := m.GetStatus()
		if status == nil || status.ProbeGenerationRatePerSecondPerAgent == 0 || len(status.AgentSpecs) == 0 {

			log.Printf("generator cannot generate directives either rate is 0 or there are no agents to select, retrying in 5 seconds")

			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return err
			}
			continue
		}

		select {
		case directive, ok := <-m.directiveGenerator.PullChannel():
			if !ok {
				return nil
			}

			interval := time.Second / time.Duration(int(status.ProbeGenerationRatePerSecondPerAgent)*len(m.GetStatus().AgentSpecs))

			if err := util.SleepWithContext(ctx, interval); err != nil {
				return err
			}

			if err := stream.Send(directive); err != nil {
				return err
			}

		case <-ctx.Done():
			return nil

		case <-m.ctx.Done():
			return nil
		}
	}
}

// Register registers this server with a gRPC server.
func (m *GeneratorManager) Register(server *grpc.Server) {
	pb.RegisterProbingDirectiveGeneratorInterfaceServer(server, m)
}

func (m *GeneratorManager) Close() error {
	m.once.Do(func() {
		m.directiveGenerator.Close()
	})
	return nil
}
