package pdg

import (
	"context"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"iprl-demo/internal/clients"
	pb "iprl-demo/internal/gen/proto"
)

type PDGServerConfig struct {
	Address string
}

type PDGServer struct {
	pb.UnimplementedPDGServiceServer

	mu        sync.RWMutex
	cfg       *PDGServerConfig
	generator PDG
	poClient  *clients.POClient
}

func NewServer(generator PDG, poClient *clients.POClient, cfg *PDGServerConfig) (*PDGServer, error) {
	s := &PDGServer{
		cfg:       cfg,
		generator: generator,
		poClient:  poClient,
	}

	return s, nil
}

func (s *PDGServer) Run(ctx context.Context) {
	// Start gRPC server
	lis, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterPDGServiceServer(grpcServer, s)

	// Register with PO
	if resp, err := s.poClient.RegisterProbingDirectiveGenerator(ctx, s.generator.GetSelf()); err != nil {
		log.Printf("warning: failed to register with PO: %v", err)
	} else {
		s.generator.SetSelf(resp.ProbingDirectiveGenerator)
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")

		// Unregister self from the PO.
		if _, err := s.poClient.UnregisterProbingDirectiveGenerator(ctx, &pb.UnregistrationRequest{
			Id: s.generator.GetSelf().Id,
		}); err != nil {
			log.Printf("failed to unregister from PO: %v", err)
		}

		grpcServer.GracefulStop()
	}()

	log.Printf("Probing Directive Generator %s listening on %s", s.generator.GetSelf().Id, s.cfg.Address)

	// Start the generator
	go s.generator.Run(ctx)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (s *PDGServer) Notify(ctx context.Context, clusterStatus *pb.ClusterStatus) (*emptypb.Empty, error) {
	s.generator.SetClusterStatus(clusterStatus)

	return &emptypb.Empty{}, nil
}
