package orchestrator

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"iprl-demo/internal/clients"
	"iprl-demo/internal/components"
	pb "iprl-demo/internal/gen/proto"
)

var (
	ErrComponentNotFound    = errors.New("component not found")
	ErrComponentExists      = errors.New("component already exists")
	ErrInvalidComponentType = errors.New("invalid component type")
	ErrDirectiveQueueFull   = errors.New("directive queue full")
	ErrElementQueueFull     = errors.New("element queue full")
)

// Compile-time checks
var _ components.OrchestratorManager = (*OrchestratorManager)(nil)
var _ pb.ProbingOrchestratorInterfaceServer = (*OrchestratorManager)(nil)

// OrchestratorManager manages the lifecycle and state of the Probing Orchestrator.
type OrchestratorManager struct {
	pb.UnimplementedProbingOrchestratorInterfaceServer

	mu sync.RWMutex

	currentSpec   *pb.ProbingOrchestratorSpec
	currentStatus *pb.ProbingOrchestratorStatus

	// Component store
	store *componentStore

	// Queues for directives and elements
	directiveCh chan *pb.ProbingDirective
	elementCh   chan *pb.ForwardingInfoElement
}

// NewOrchestratorManager creates a new OrchestratorManager with the given specification.
func NewOrchestratorManager(spec *pb.ProbingOrchestratorSpec) *OrchestratorManager {
	return &OrchestratorManager{
		currentSpec: spec,
		currentStatus: &pb.ProbingOrchestratorStatus{
			GlobalProbingRateCap: spec.DefaultGlobalProbingRateCap,
		},
		store:       newComponentStore(),
		directiveCh: make(chan *pb.ProbingDirective, spec.DirectiveBufferLength),
		elementCh:   make(chan *pb.ForwardingInfoElement, spec.ElementBufferLength),
	}
}

// GetSpec returns the orchestrator's current specification.
func (m *OrchestratorManager) GetSpec() *pb.ProbingOrchestratorSpec {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentSpec
}

// SetSpec updates the orchestrator's specification.
func (m *OrchestratorManager) SetSpec(spec *pb.ProbingOrchestratorSpec) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentSpec = spec
}

// GetStatus returns the orchestrator's current status.
func (m *OrchestratorManager) GetStatus() *pb.ProbingOrchestratorStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentStatus
}

// SetStatus updates the orchestrator's status.
func (m *OrchestratorManager) SetStatus(status *pb.ProbingOrchestratorStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentStatus = status
}

// RegisterComponent registers a new agent or generator with the orchestrator.
func (m *OrchestratorManager) RegisterComponent(ctx context.Context, req *pb.RegisterComponentRequest) (*pb.RegisterComponentResponse, error) {
	switch comp := req.Component.(type) {
	case *pb.RegisterComponentRequest_ProbingAgentSpec:
		return m.registerAgent(comp.ProbingAgentSpec)
	case *pb.RegisterComponentRequest_ProbingDirectiveGeneratorSpec:
		return m.registerGenerator(comp.ProbingDirectiveGeneratorSpec)
	default:
		return nil, ErrInvalidComponentType
	}
}

func (m *OrchestratorManager) registerAgent(spec *pb.ProbingAgentSpec) (*pb.RegisterComponentResponse, error) {
	id := uuid.New().String()

	status := &pb.ProbingAgentStatus{
		Uuid:           id,
		ProbingRateCap: m.GetStatus().GlobalProbingRateCap,
		Tags:           make(map[string]string),
	}

	if err := m.store.RegisterAgent(spec, status); err != nil {
		return nil, err
	}

	log.Printf("registered agent: uuid=%s, vantage_point=%s", id, spec.VantagePoint.Name)

	return &pb.RegisterComponentResponse{
		Component: &pb.RegisterComponentResponse_ProbingAgentStatus{
			ProbingAgentStatus: status,
		},
	}, nil
}

func (m *OrchestratorManager) registerGenerator(spec *pb.ProbingDirectiveGeneratorSpec) (*pb.RegisterComponentResponse, error) {
	id := uuid.New().String()

	// Get current agent specs for the generator
	agentSpecs, _ := m.store.ListAgents()

	status := &pb.ProbingDirectiveGeneratorStatus{
		Uuid:                                 id,
		ProbeGenerationRatePerSecondPerAgent: m.GetSpec().DefaultGlobalProbingRateCap,
		AgentSpecs:                           agentSpecs,
		Tags:                                 make(map[string]string),
	}

	if err := m.store.RegisterGenerator(spec, status); err != nil {
		return nil, err
	}

	log.Printf("registered generator: uuid=%s", id)

	return &pb.RegisterComponentResponse{
		Component: &pb.RegisterComponentResponse_ProbingDirectiveGeneratorStatus{
			ProbingDirectiveGeneratorStatus: status,
		},
	}, nil
}

// UnregisterComponent removes a component from the orchestrator.
func (m *OrchestratorManager) UnregisterComponent(ctx context.Context, req *pb.UnRegisterComponentRequest) error {
	// Try to delete as agent first
	if err := m.store.DeleteAgent(req.Uuid); err == nil {
		log.Printf("unregistered agent: uuid=%s", req.Uuid)
		return nil
	}

	// Try as generator
	if err := m.store.DeleteGenerator(req.Uuid); err == nil {
		log.Printf("unregistered generator: uuid=%s", req.Uuid)
		return nil
	}

	return ErrComponentNotFound
}

// EnqueueDirective adds a probing directive to the distribution queue.
func (m *OrchestratorManager) EnqueueDirective(ctx context.Context, directive *pb.ProbingDirective) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.directiveCh <- directive:
		return nil
	}
}

// EnqueueElement adds a forwarding information element to the collection queue.
func (m *OrchestratorManager) EnqueueElement(ctx context.Context, element *pb.ForwardingInfoElement) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case m.elementCh <- element:
		return nil
	}
}

// RegisterComponent handles gRPC registration requests.
// Note: This wraps the OrchestratorManager method for gRPC compatibility.
// The interface method is already named RegisterComponent, so this satisfies both.

// UnRegisterComponent handles gRPC unregistration requests.
func (m *OrchestratorManager) UnRegisterComponent(ctx context.Context, req *pb.UnRegisterComponentRequest) (*emptypb.Empty, error) {
	if err := m.UnregisterComponent(ctx, req); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// PushProbingDirectives receives directives from generators.
func (m *OrchestratorManager) PushProbingDirectives(stream grpc.ClientStreamingServer[pb.ProbingDirective, emptypb.Empty]) error {
	ctx := stream.Context()

	for {
		directive, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				return nil
			}
			return err
		}

		if err := m.EnqueueDirective(ctx, directive); err != nil {
			log.Printf("failed to enqueue directive: %v", err)
		}
	}
}

// PushForwardingInfoElements handles bidirectional streaming with agents.
// Receives elements from agents and sends directives to them.
func (m *OrchestratorManager) PushForwardingInfoElements(stream grpc.BidiStreamingServer[pb.ForwardingInfoElement, pb.ProbingDirective]) error {
	ctx := stream.Context()

	errCh := make(chan error, 2)

	// Receive elements from agent
	go func() {
		for {
			element, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}

			if err := m.EnqueueElement(ctx, element); err != nil {
				log.Printf("failed to enqueue element: %v", err)
			}
		}
	}()

	// Send directives to agent
	go func() {
		for {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case directive := <-m.directiveCh:
				if err := stream.Send(directive); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	// Wait for first error
	err := <-errCh
	if err != nil && err.Error() != "EOF" {
		return err
	}
	return nil
}

// PullForwardingInfoElements streams collected elements to subscribers.
func (m *OrchestratorManager) PullForwardingInfoElements(_ *emptypb.Empty, stream grpc.ServerStreamingServer[pb.ForwardingInfoElement]) error {
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case element := <-m.elementCh:
			if err := stream.Send(element); err != nil {
				return err
			}
		}
	}
}

// Run starts the orchestrator's main processing loop.
func (m *OrchestratorManager) Run(ctx context.Context) error {
	log.Println("orchestrator started")

	// Start background tasks
	go m.runNotificationLoop(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	log.Println("orchestrator stopped")
	return ctx.Err()
}

// runNotificationLoop periodically notifies components of status changes.
func (m *OrchestratorManager) runNotificationLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.notifyChangedAgents(ctx)
			m.notifyChangedGenerators(ctx)
		}
	}
}

func (m *OrchestratorManager) notifyChangedAgents(ctx context.Context) {
	changedStatuses := m.store.GetChangedAgents()
	if len(changedStatuses) == 0 {
		return
	}

	for _, status := range changedStatuses {
		spec, _, err := m.store.GetAgent(status.Uuid)
		if err != nil {
			continue
		}

		go func(spec *pb.ProbingAgentSpec, status *pb.ProbingAgentStatus) {
			client, conn, err := clients.NewInsecureAgentClient(spec.InterfaceAddr)
			if err != nil {
				log.Printf("failed to connect to agent %s: %v", status.Uuid, err)
				return
			}
			defer conn.Close()

			_, err = client.Update(ctx, status)
			if err != nil {
				log.Printf("failed to notify agent %s: %v", status.Uuid, err)
				return
			}

			log.Printf("notified agent %s", status.Uuid)
		}(spec, status)
	}
}

func (m *OrchestratorManager) notifyChangedGenerators(ctx context.Context) {
	changedStatuses := m.store.GetChangedGenerators()
	if len(changedStatuses) == 0 {
		return
	}

	// Get current agent specs to include in generator status
	agentSpecs, _ := m.store.ListAgents()

	for _, status := range changedStatuses {
		spec, _, err := m.store.GetGenerator(status.Uuid)
		if err != nil {
			continue
		}

		// Update agent specs in status
		status.AgentSpecs = agentSpecs

		go func(spec *pb.ProbingDirectiveGeneratorSpec, status *pb.ProbingDirectiveGeneratorStatus) {
			client, conn, err := clients.NewInsecureGeneratorClient(spec.InterfaceAddr)
			if err != nil {
				log.Printf("failed to connect to generator %s: %v", status.Uuid, err)
				return
			}
			defer conn.Close()

			_, err = client.Update(ctx, status)
			if err != nil {
				log.Printf("failed to notify generator %s: %v", status.Uuid, err)
				return
			}

			log.Printf("notified generator %s", status.Uuid)
		}(spec, status)
	}
}

// Register registers the orchestrator with a gRPC server.
func (m *OrchestratorManager) Register(server *grpc.Server) {
	pb.RegisterProbingOrchestratorInterfaceServer(server, m)
}

// Close closes the orchestrator's channels.
func (m *OrchestratorManager) Close() {
	close(m.directiveCh)
	close(m.elementCh)
}
