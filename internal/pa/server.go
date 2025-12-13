package pa

import (
	"context"
	"io"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"iprl-demo/internal/clients"
	pb "iprl-demo/internal/gen/proto"
)

var ErrProbeAlreadyActive = status.Error(codes.ResourceExhausted, "probe stream already active")

type PAServerConfig struct {
	Address string
}

type PAServer struct {
	pb.UnimplementedPAServiceServer

	mu       sync.RWMutex
	cfg      *PAServerConfig
	manager  PA
	poClient *clients.POClient
	probing  bool

	exitCtx context.Context
}

func NewPAServer(manager PA, poClient *clients.POClient, cfg *PAServerConfig) (*PAServer, error) {
	s := &PAServer{
		cfg:      cfg,
		manager:  manager,
		poClient: poClient,
		probing:  false,
	}

	return s, nil
}

func (s *PAServer) Run(ctx context.Context) {
	// Start gRPC server
	lis, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterPAServiceServer(grpcServer, s)

	s.exitCtx = ctx

	// Register with PO
	if resp, err := s.poClient.RegisterProbingAgent(ctx, s.manager.GetSelf()); err != nil {
		log.Printf("warning: failed to register with PO: %v", err)
	} else {
		s.manager.SetSelf(resp.ProbingAgent)
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")

		// Unregister self from the PO.
		if _, err := s.poClient.UnregisterProbingDirectiveGenerator(ctx, &pb.UnregistrationRequest{
			Id: s.manager.GetSelf().Id,
		}); err != nil {
			log.Printf("failed to unregister from PO: %v", err)
		}

		grpcServer.GracefulStop()
	}()

	log.Printf("Probing Directive Generator %s listening on %s", s.manager.GetSelf().Id, s.cfg.Address)

	// Start the probing
	go s.manager.Run(ctx)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (s *PAServer) Notify(ctx context.Context, clusterStatus *pb.ClusterStatus) (*emptypb.Empty, error) {
	s.manager.SetClusterStatus(clusterStatus)

	return &emptypb.Empty{}, nil
}

func (s *PAServer) Probe(stream grpc.BidiStreamingServer[pb.ProbingDirective, pb.ForwardingInfoElement]) error {
	// Ensure only one active probe connection
	s.mu.Lock()
	if s.probing {
		s.mu.Unlock()
		return ErrProbeAlreadyActive
	}
	s.probing = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.probing = false
		s.mu.Unlock()
	}()

	directiveCh := s.manager.DirectiveChannel()

	// Goroutine to send results back
	for {
		directive, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-s.exitCtx.Done():
			return s.exitCtx.Err()
		case directiveCh <- directive:
			continue // directive added to the streaming queue.
		}
	}
}
