package servers

import (
	"context"
	"iprl-demo/internal/components/orchestrator"
	pb "iprl-demo/internal/gen/proto"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type OrchestratorServer struct {
	pb.UnimplementedProbingOrchestratorInterfaceServer

	probingOrchestrator orchestrator.ProbingOrchestrator
}

func NewOrchestratorServer(probingOrchestrator orchestrator.ProbingOrchestrator, spec *pb.ProbingOrchestratorSpec) *OrchestratorServer {
	return &OrchestratorServer{
		probingOrchestrator: probingOrchestrator,
	}
}

func (s *OrchestratorServer) PullForwardingInfoElements(_ *emptypb.Empty, stream grpc.ServerStreamingServer[pb.ForwardingInfoElement]) error {
	return nil // TODO
}

func (s *OrchestratorServer) PushForwardingInfoElements(grpc.BidiStreamingServer[pb.ForwardingInfoElement, pb.ProbingDirective]) error {
	return nil // TODO
}

func (s *OrchestratorServer) PushProbingDirectives(stream grpc.ClientStreamingServer[pb.ProbingDirective, emptypb.Empty]) error {
	return nil // TODO
}

func (s *OrchestratorServer) RegisterComponent(ctx context.Context, req *pb.RegisterComponentRequest) (*pb.RegisterComponentResponse, error) {
	return s.probingOrchestrator.RegisterComponent(req)
}

func (s *OrchestratorServer) UnRegisterComponent(ctx context.Context, req *pb.UnRegisterComponentRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.probingOrchestrator.UnRegisterComponent(req)
}

func (s *OrchestratorServer) Run(ctx context.Context) {
	lis, err := net.Listen("tcp", s.probingOrchestrator.GetSpec().InterfaceAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterProbingOrchestratorInterfaceServer(grpcServer, s)

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")
		<-s.probingOrchestrator.ExitContext().Done()
		grpcServer.GracefulStop()
	}()

	log.Printf("Probing Orchestrator Server is listening on %s", s.probingOrchestrator.GetSpec().InterfaceAddr)

	go s.probingOrchestrator.Run(ctx)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
