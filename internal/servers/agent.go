package servers

// import (
// 	"context"
// 	"iprl-demo/internal/components/agent"
// 	pb "iprl-demo/internal/gen/proto"
// 	"log"
// 	"net"
//
// 	"google.golang.org/grpc"
// 	"google.golang.org/protobuf/types/known/emptypb"
// )
//
// type AgentServer struct {
// 	pb.UnimplementedProbingAgentInterfaceServer
//
// 	probingAgent agent.ProbingAgent
// }
//
// func NewAgentServer(probingDirectiveAgent agent.ProbingAgent, spec *pb.ProbingAgentSpec) *AgentServer {
// 	return &AgentServer{
// 		probingAgent: probingDirectiveAgent,
// 	}
// }
//
// func (s *AgentServer) Update(ctx context.Context, status *pb.ProbingAgentStatus) (*emptypb.Empty, error) {
// 	s.probingAgent.SetStatus(status)
// 	return &emptypb.Empty{}, nil
// }
//
// func (s *AgentServer) Run(ctx context.Context) {
// 	lis, err := net.Listen("tcp", s.probingAgent.GetSpec().InterfaceAddr)
// 	if err != nil {
// 		log.Fatalf("failed to listen: %v", err)
// 	}
//
// 	grpcServer := grpc.NewServer()
// 	pb.RegisterProbingAgentInterfaceServer(grpcServer, s)
//
// 	// Graceful shutdown
// 	go func() {
// 		<-ctx.Done()
// 		log.Println("Shutting down...")
// 		<-s.probingAgent.ExitContext().Done()
// 		grpcServer.GracefulStop()
// 	}()
//
// 	log.Printf("Probing Agent Server is listening on %s", s.probingAgent.GetSpec().InterfaceAddr)
//
// 	go s.probingAgent.Run(ctx)
//
// 	if err := grpcServer.Serve(lis); err != nil {
// 		log.Fatalf("failed to serve: %v", err)
// 	}
// }
