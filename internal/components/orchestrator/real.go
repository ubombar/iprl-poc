package orchestrator

import (
	"context"
	"errors"
	pb "iprl-demo/internal/gen/proto"
	"log"
	"sync"

	"github.com/google/uuid"
)

type RealProbingOrchestrator struct {
	mu            sync.RWMutex
	exitCtx       context.Context
	exitCtxCancel context.CancelFunc

	store         *componentStore
	currentSpec   *pb.ProbingOrchestratorSpec
	currentStatus *pb.ProbingOrchestratorStatus

	directiveCh chan *pb.ProbingDirective
	elementCh   chan *pb.ForwardingInfoElement
}

var _ ProbingOrchestrator = (*RealProbingOrchestrator)(nil)

func NewRealProbingOrchestrator(spec *pb.ProbingOrchestratorSpec) *RealProbingOrchestrator {
	exitCtx, exitCtxCancel := context.WithCancel(context.Background())

	status := &pb.ProbingOrchestratorStatus{
		GlobalProbingRateCap: spec.DefaultGlobalProbingRateCap,
	}

	return &RealProbingOrchestrator{
		exitCtx:       exitCtx,
		exitCtxCancel: exitCtxCancel,

		store:         newComponentStore(),
		currentSpec:   spec,
		currentStatus: status,

		directiveCh: make(chan *pb.ProbingDirective, spec.DirectiveBufferLength),
		elementCh:   make(chan *pb.ForwardingInfoElement, spec.ElementBufferLength),
	}
}

// This gets the spec object.
func (m *RealProbingOrchestrator) GetSpec() *pb.ProbingOrchestratorSpec {
	return m.currentSpec
}

// This sets the status object
func (m *RealProbingOrchestrator) SetStatus(s *pb.ProbingOrchestratorStatus) {
	m.currentStatus = s
}

// This gets the spec object.
func (m *RealProbingOrchestrator) GetProbingDirectiveChannel() chan *pb.ProbingDirective {
	return m.directiveCh
}

// This sets the status object
func (m *RealProbingOrchestrator) GetForwardingInfoElementChannel() chan *pb.ForwardingInfoElement {
	return m.elementCh
}

// Registers the component
func (m *RealProbingOrchestrator) RegisterComponent(req *pb.RegisterComponentRequest) (*pb.RegisterComponentResponse, error) {
	switch comp := req.Component.(type) {
	case *pb.RegisterComponentRequest_ProbingAgentSpec:
		return m.registerAgent(comp.ProbingAgentSpec)
	case *pb.RegisterComponentRequest_ProbingDirectiveGeneratorSpec:
		return m.registerGenerator(comp.ProbingDirectiveGeneratorSpec)
	default:
		return nil, ErrInvalidComponentType
	}
}

func (m *RealProbingOrchestrator) registerAgent(spec *pb.ProbingAgentSpec) (*pb.RegisterComponentResponse, error) {
	uuid := generateUUID()

	status := &pb.ProbingAgentStatus{
		Uuid:           uuid,
		ProbingRateCap: m.currentStatus.GlobalProbingRateCap,
		Tags:           make(map[string]string),
	}

	if err := m.store.RegisterAgent(spec, status); err != nil {
		return nil, err
	}

	return &pb.RegisterComponentResponse{
		Component: &pb.RegisterComponentResponse_ProbingAgentStatus{
			ProbingAgentStatus: status,
		},
	}, nil
}

func (m *RealProbingOrchestrator) registerGenerator(spec *pb.ProbingDirectiveGeneratorSpec) (*pb.RegisterComponentResponse, error) {
	uuid := generateUUID()

	status := &pb.ProbingDirectiveGeneratorStatus{
		Uuid:                                 uuid,
		ProbeGenerationRatePerSecondPerAgent: m.currentStatus.GlobalProbingRateCap,
		Tags:                                 make(map[string]string),
	}

	if err := m.store.RegisterGenerator(spec, status); err != nil {
		return nil, err
	}

	return &pb.RegisterComponentResponse{
		Component: &pb.RegisterComponentResponse_ProbingDirectiveGeneratorStatus{
			ProbingDirectiveGeneratorStatus: status,
		},
	}, nil
}

func generateUUID() string {
	return uuid.New().String()
}

// UnRegisters the component
func (m *RealProbingOrchestrator) UnRegisterComponent(req *pb.UnRegisterComponentRequest) error {
	// Try to delete as agent first
	err := m.store.DeleteAgent(req.Uuid)
	if err == nil {
		return nil
	}

	// If not found as agent, try as generator
	if errors.Is(err, ErrComponentNotFound) {
		err = m.store.DeleteGenerator(req.Uuid)
		if err == nil {
			return nil
		}
	}

	return err
}

// Used for the caller to wait for this to end execution.
func (m *RealProbingOrchestrator) ExitContext() context.Context {
	return m.exitCtx
}

// Runs the logic in the same Go routine.
func (m *RealProbingOrchestrator) Run(ctx context.Context) {
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

		// We need to consume the queue here
		// go m.streamDirectives(ctx)
		// go m.streamDirectives(ctx)
	}
}
