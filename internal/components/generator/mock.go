package generator

import (
	"context"
	"iprl-demo/internal/clients"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"log"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type MockProbingDirectiveGenerator struct {
	mu            sync.RWMutex
	exitCtx       context.Context
	exitCtxCancel context.CancelFunc

	currentSpec   *pb.ProbingDirectiveGeneratorSpec
	currentStatus *pb.ProbingDirectiveGeneratorStatus
}

var _ ProbingDirectiveGenerator = (*MockProbingDirectiveGenerator)(nil)

func NewMockProbingDirectiveGenerator(spec *pb.ProbingDirectiveGeneratorSpec) *MockProbingDirectiveGenerator {
	exitCtx, exitCtxCancel := context.WithCancel(context.Background())

	return &MockProbingDirectiveGenerator{
		exitCtx:       exitCtx,
		exitCtxCancel: exitCtxCancel,

		currentSpec:   spec,
		currentStatus: nil,
	}
}

// This gets the spec object.
func (m *MockProbingDirectiveGenerator) GetSpec() *pb.ProbingDirectiveGeneratorSpec {
	return m.currentSpec
}

// This sets the status object
func (m *MockProbingDirectiveGenerator) SetStatus(s *pb.ProbingDirectiveGeneratorStatus) {
	m.currentStatus = s
}

// Used for the caller to wait for this to end execution.
func (m *MockProbingDirectiveGenerator) ExitContext() context.Context {
	return m.exitCtx
}

// Runs the logic in the same Go routine.
func (m *MockProbingDirectiveGenerator) Run(ctx context.Context) {
	select {
	case <-m.exitCtx.Done():
		return // cannot run a second time.
	default:
	}

	defer m.exitCtxCancel() // ??
	numRetries := uint32(0)

	for {
		// Check if the num retries are exceeded
		if m.currentSpec.NumRetries != 0 && numRetries > m.currentSpec.NumRetries {
			log.Println("max number of retries are exceeded, exitting")
			return
		}

		// Check if the context is cancelled
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Try to make a connection with the orchestrator
		client, conn, err := clients.NewInsecureOrchestratorClient(m.currentSpec.OrchestratorAddress)
		if err != nil {
			numRetries += 1
			log.Printf("failed to open connection with the orchestrator (num_tries=%d): %v", numRetries, err)
			conn.Close()
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return
			}
			continue
		}

		// Try to register self to the orchestrator
		res, err := client.RegisterComponent(ctx, &pb.RegisterComponentRequest{
			Component: &pb.RegisterComponentRequest_ProbingDirectiveGeneratorSpec{
				ProbingDirectiveGeneratorSpec: m.currentSpec,
			},
		})
		if err != nil {
			numRetries += 1
			log.Printf("failed to register generator with the orchestrator (num_tries=%d): %v", numRetries, err)
			conn.Close()
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return
			}
			continue
		}

		if res.GetProbingDirectiveGeneratorStatus() == nil {
			log.Printf("failed to get spec from orchestrator, this is likely a bug on the orchestrator side")
			conn.Close()
			return
		}
		m.SetStatus(res.GetProbingDirectiveGeneratorStatus())

		// Try to establish a stream
		stream, err := client.PushProbingDirectives(ctx)
		if err != nil {
			numRetries += 1
			log.Printf("failed to open stream with the orchestrator (num_tries=%d): %v", numRetries, err)
			conn.Close()
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return
			}
			continue
		}

		m.streamDirectives(ctx, stream)

		if err := stream.CloseSend(); err != nil {
			numRetries += 1
			log.Printf("failed to close stream with the orchestrator (num_tries=%d): %v", numRetries, err)
			conn.Close()
			if err := util.SleepWithContext(ctx, 5*time.Second); err != nil {
				return
			}
			continue
		}
	}
}

func (m *MockProbingDirectiveGenerator) streamDirectives(ctx context.Context, stream grpc.ClientStreamingClient[pb.ProbingDirective, emptypb.Empty]) {
	// TODO
}
