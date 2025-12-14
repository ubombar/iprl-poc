package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
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

// EnqueueDirective adds a probing directive to the distribution queue.
func (m *OrchestratorManager) EnqueueDirective(ctx context.Context, directive *pb.ProbingDirective) {
	select {
	case <-ctx.Done():
		return
	case m.directiveCh <- directive:
		return
	}
}

// EnqueueElement adds a forwarding information element to the collection queue.
func (m *OrchestratorManager) EnqueueElement(ctx context.Context, element *pb.ForwardingInfoElement) {
	select {
	case <-ctx.Done():
		return
	case m.elementCh <- element:
		return
	}
}

func (m *OrchestratorManager) Join(ctx context.Context, req *pb.JoinRequest) (*pb.JoinResponse, error) {
	switch comp := req.Spec.(type) {
	case *pb.JoinRequest_ProbingAgentSpec:
		return m.joinAgent(comp.ProbingAgentSpec)
	case *pb.JoinRequest_ProbingDirectiveGeneratorSpec:
		return m.joinGenerator(comp.ProbingDirectiveGeneratorSpec)
	default:
		return nil, ErrInvalidComponentType
	}
}

func (m *OrchestratorManager) Leave(ctx context.Context, req *pb.LeaveRequest) (*emptypb.Empty, error) {
	if err := m.store.RemoveAgent(req.Uuid); err == nil {
		log.Printf("unregistered agent with uuid=%s", req.Uuid)
		return &emptypb.Empty{}, nil
	}

	if err := m.store.RemoveGenerator(req.Uuid); err == nil {
		log.Printf("unregistered generator with uuid=%s", req.Uuid)
		return &emptypb.Empty{}, nil
	}

	return &emptypb.Empty{}, ErrComponentNotFound
}

func (m *OrchestratorManager) joinAgent(spec *pb.ProbingAgentSpec) (*pb.JoinResponse, error) {
	id := uuid.New().String()

	status := &pb.ProbingAgentStatus{
		Uuid:           id,
		ProbingRateCap: m.GetStatus().GlobalProbingRateCap,
		Tags:           make(map[string]string),
	}

	if err := m.store.AddOrUpdateAgent(spec, status); err != nil { // Change the name later
		return nil, err
	}

	log.Printf("registered agent with uuid=%s", id)

	return &pb.JoinResponse{
		Status: &pb.JoinResponse_ProbingAgentStatus{
			ProbingAgentStatus: status,
		},
	}, nil
}

func (m *OrchestratorManager) joinGenerator(spec *pb.ProbingDirectiveGeneratorSpec) (*pb.JoinResponse, error) {
	id := uuid.New().String()

	// Get current agent specs for the generator
	agentSpecs := m.store.ListAgents()

	status := &pb.ProbingDirectiveGeneratorStatus{
		Uuid:                                 id,
		ProbeGenerationRatePerSecondPerAgent: m.GetSpec().DefaultGlobalProbingRateCap,
		AgentSpecs:                           agentSpecs,
		Tags:                                 make(map[string]string),
	}

	if err := m.store.AddOrUpdateGenerator(spec, status); err != nil {
		return nil, err
	}

	log.Printf("registered generator with uuid=%s", id)

	return &pb.JoinResponse{
		Status: &pb.JoinResponse_ProbingDirectiveGeneratorStatus{
			ProbingDirectiveGeneratorStatus: status,
		},
	}, nil
}

// PushProbingDirectives receives directives from generators.
func (m *OrchestratorManager) PushProbingDirectives(stream grpc.ClientStreamingServer[pb.ProbingDirective, emptypb.Empty]) error {
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		directive, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		m.EnqueueDirective(ctx, directive)
	}
}

// PushForwardingInfoElements handles bidirectional streaming with agents.
// Receives elements from agents and sends directives to them.
func (m *OrchestratorManager) PushForwardingInfoElements(stream grpc.BidiStreamingServer[pb.ForwardingInfoElement, pb.ProbingDirective]) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// Receive elements from agent
	g.Go(func() error {
		defer cancel() // Cancel context when receive ends
		for {
			select {
			case <-ctx.Done():
				return nil
			default:
			}

			element, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}

			m.EnqueueElement(ctx, element)
		}
	})

	// Send directives to agent
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil // Exit cleanly on context cancellation
			case directive := <-m.directiveCh:
				if err := stream.Send(directive); err != nil {
					return err
				}
			}
		}
	})

	return g.Wait()
}

// PullForwardingInfoElements streams collected elements to subscribers.
func (m *OrchestratorManager) PullForwardingInfoElements(_ *emptypb.Empty, stream grpc.ServerStreamingServer[pb.ForwardingInfoElement]) error {
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return nil
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
	go m.runStreamToStdout(ctx)

	// Wait for context cancellation
	<-ctx.Done()

	log.Println("orchestrator stopped")
	return nil
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
	dirtyUuids := m.store.GetDirtyAgentUuids()
	if len(dirtyUuids) == 0 {
		return
	}

	for _, uuid := range dirtyUuids {
		spec, status, err := m.store.GetAgent(uuid)
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
	dirtyUuids := m.store.GetDirtyGeneratorUuids()
	if len(dirtyUuids) == 0 {
		return
	}

	for _, uuid := range dirtyUuids {
		spec, status, err := m.store.GetGenerator(uuid)
		if err != nil {
			continue
		}

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

func (m *OrchestratorManager) runStreamToStdout(ctx context.Context) {
	encoder := json.NewEncoder(os.Stdout)

	for {
		select {
		case <-ctx.Done():
			return
		case element, ok := <-m.elementCh:
			if !ok {
				return
			}
			if err := encoder.Encode(element); err != nil {
				log.Printf("failed to encode element to stdout: %v", err)
			}
		}
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
