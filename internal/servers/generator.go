package servers

import (
	"context"
	"iprl-demo/internal/components/generator"
	pb "iprl-demo/internal/gen/proto"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

type GeneratorServer struct {
	pb.UnimplementedProbingDirectiveGeneratorInterfaceServer

	probingDirectiveGenerator generator.ProbingDirectiveGenerator
}

func NewGeneratorServer(probingDirectiveGenerator generator.ProbingDirectiveGenerator, spec *pb.ProbingDirectiveGeneratorSpec) *GeneratorServer {
	return &GeneratorServer{
		probingDirectiveGenerator: probingDirectiveGenerator,
	}
}

func (s *GeneratorServer) Update(ctx context.Context, status *pb.ProbingDirectiveGeneratorStatus) (*emptypb.Empty, error) {
	s.probingDirectiveGenerator.SetStatus(status)
	return &emptypb.Empty{}, nil
}

func (s *GeneratorServer) Run(ctx context.Context) {
	lis, err := net.Listen("tcp", s.probingDirectiveGenerator.GetSpec().InterfaceAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterProbingDirectiveGeneratorInterfaceServer(grpcServer, s)

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")
		<-s.probingDirectiveGenerator.ExitContext().Done()
		grpcServer.GracefulStop()
	}()

	log.Printf("Probing Directive Generator Server is listening on %s", s.probingDirectiveGenerator.GetSpec().InterfaceAddr)

	go s.probingDirectiveGenerator.Run(ctx)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
