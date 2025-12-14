package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"iprl-demo/internal/api"
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

	mu   sync.RWMutex
	once sync.Once
	ctx  context.Context

	currentSpec   *pb.ProbingOrchestratorSpec
	currentStatus *pb.ProbingOrchestratorStatus

	// Component store
	store *componentStore

	// Queues for directives and elements
	directiveCh chan *pb.ProbingDirective
	elementCh   chan *pb.ForwardingInfoElement

	// HTTP streaming clients
	clients map[string]chan *pb.ForwardingInfoElement
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
		clients:     make(map[string]chan *pb.ForwardingInfoElement, 10),
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
		log.Printf("agent with uuid=%s left", req.Uuid)
		return &emptypb.Empty{}, nil
	}

	if err := m.store.RemoveGenerator(req.Uuid); err == nil {
		log.Printf("generator with uuid=%s left", req.Uuid)
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

	log.Printf("agent with uuid=%s joined", id)

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

	log.Printf("generator with uuid=%s joined", id)

	return &pb.JoinResponse{
		Status: &pb.JoinResponse_ProbingDirectiveGeneratorStatus{
			ProbingDirectiveGeneratorStatus: status,
		},
	}, nil
}

func (m *OrchestratorManager) PushProbingDirectives(stream grpc.ClientStreamingServer[pb.ProbingDirective, emptypb.Empty]) error {
	ctx := stream.Context()

	for {
		directive, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				// client finished sending
				return stream.SendAndClose(&emptypb.Empty{})
			}
			return err
		}

		// Non-blocking / cancellation-aware enqueue
		select {
		case <-ctx.Done():
			return nil
		case m.directiveCh <- directive:
			// ok
		}
	}
}

func (m *OrchestratorManager) PushForwardingInfoElements(stream grpc.BidiStreamingServer[pb.ForwardingInfoElement, pb.ProbingDirective]) error {
	g, ctx := errgroup.WithContext(stream.Context())

	// Receive elements from agent
	g.Go(func() error {
		for {
			elem, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}

			select {
			case <-ctx.Done():
				return nil
			case <-m.ctx.Done():
				return nil
			case m.elementCh <- elem:
				// ok
			}
		}
	})

	// Send directives to agent
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil

			case <-m.ctx.Done():
				return nil

			case directive, ok := <-m.directiveCh:
				if !ok {
					// orchestrator shutting down
					return nil
				}

				if err := stream.Send(directive); err != nil {
					return err
				}
			}
		}
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}
	return nil
}

// Run starts the orchestrator's main processing loop.
func (m *OrchestratorManager) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	m.ctx = ctx

	// Start all go routines, if one fails context is cancelled for everyone.
	g.Go(func() error {
		return m.runNotificationLoop(ctx)
	})

	g.Go(func() error {
		return m.runFanout(ctx)
	})

	g.Go(func() error {
		return m.runRPCServer(ctx)
	})

	g.Go(func() error {
		return m.runHTTPServer(ctx)
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

// runNotificationLoop periodically notifies components of status changes.
func (m *OrchestratorManager) runNotificationLoop(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := m.notifyChangedAgents(ctx); err != nil {
				return err
			}
			if err := m.notifyChangedGenerators(ctx); err != nil {
				return err
			}
		}
	}
}

func (m *OrchestratorManager) notifyChangedAgents(ctx context.Context) error {
	dirtyUuids := m.store.GetDirtyAgentUuids()

	// No dirty agent to notify
	if len(dirtyUuids) == 0 {
		return nil
	}

	for _, uuid := range dirtyUuids {
		spec, status, err := m.store.GetAgent(uuid)
		if err != nil {
			continue
		}

		go func(spec *pb.ProbingAgentSpec, status *pb.ProbingAgentStatus) {
			client, conn, err := clients.NewInsecureAgentClient(spec.GrpcAddress)
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

	return nil
}

func (m *OrchestratorManager) notifyChangedGenerators(ctx context.Context) error {
	dirtyUuids := m.store.GetDirtyGeneratorUuids()
	if len(dirtyUuids) == 0 {
		return nil
	}

	for _, uuid := range dirtyUuids {
		spec, status, err := m.store.GetGenerator(uuid)
		if err != nil {
			continue
		}

		go func(spec *pb.ProbingDirectiveGeneratorSpec, status *pb.ProbingDirectiveGeneratorStatus) {
			client, conn, err := clients.NewInsecureGeneratorClient(spec.GrpcAddress)
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
	return nil
}

func (m *OrchestratorManager) runFanout(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case elem, ok := <-m.elementCh:
			if !ok {
				return errors.New("element channel is closed")
			}

			m.mu.RLock()
			for _, ch := range m.clients {
				select {
				case ch <- elem:
					// delivered
				default:
					// client is slow → drop: we need to see what happens if the client is too slow to
					// consume the elements, should we drop the first added? last added? etc.
				}
			}
			m.mu.RUnlock()

			// Maybe we can push it to some file later?
		}
	}
}

func (m *OrchestratorManager) runRPCServer(ctx context.Context) error {
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

func (m *OrchestratorManager) runHTTPServer(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", m.Stream)

	httpServer := &http.Server{
		Addr:    m.currentSpec.HttpAddress,
		Handler: mux,
	}

	log.Printf("http server listening on %s", m.currentSpec.HttpAddress)

	go func() {
		<-ctx.Done()
		log.Println("http shutdown initiated")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("http graceful shutdown failed: %v", err)
		}

		// If still hanging after timeout, force close
		if shutdownCtx.Err() == context.DeadlineExceeded {
			_ = httpServer.Close()
		}
	}()

	err := httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Register registers the orchestrator with a gRPC server.
func (m *OrchestratorManager) Register(server *grpc.Server) {
	pb.RegisterProbingOrchestratorInterfaceServer(server, m)
}

func (m *OrchestratorManager) Close() error {
	m.once.Do(func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.directiveCh != nil {
			close(m.directiveCh)
			m.directiveCh = nil
		}

		if m.elementCh != nil {
			close(m.elementCh)
			m.elementCh = nil
		}

		// The m.client is manager by the Stream function to prevent double free.
	})

	return nil
}

// Stream implements HTTPStreamer interface for SSE streaming of ForwardingInfoElements
func (m *OrchestratorManager) Stream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientID := uuid.NewString()
	clientCh := make(chan *pb.ForwardingInfoElement, 128)

	m.mu.Lock()
	m.clients[clientID] = clientCh
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.clients, clientID)
		close(clientCh)
		m.mu.Unlock()
	}()

	// ---- Stream loop ----
	enc := json.NewEncoder(w)

	for {
		select {
		case <-ctx.Done():
			return

		case <-m.ctx.Done():
			return

		case elem, ok := <-clientCh:
			if !ok {
				return
			}

			view := api.FromProto(elem)
			if view == nil {
				continue
			}

			if err := enc.Encode(view); err != nil {
				// client disconnected or write error
				return
			}

			flusher.Flush()
		}
	}
}
