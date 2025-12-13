package po

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "iprl-demo/internal/gen/proto"
)

type POServerConfig struct {
	Address string
}

type POServer struct {
	pb.UnimplementedPOServiceServer

	mu      sync.RWMutex
	cfg     *POServerConfig
	manager *POManager

	exitCtx context.Context
}

func NewPOServer(manager *POManager, cfg *POServerConfig) (*POServer, error) {
	s := &POServer{
		cfg:     cfg,
		manager: manager,
	}

	return s, nil
}

func (s *POServer) Run(ctx context.Context) {
	// Start gRPC server
	lis, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterPOServiceServer(grpcServer, s)
	s.exitCtx = ctx

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		log.Println("Shutting down...")

		grpcServer.GracefulStop()
	}()

	log.Printf("Probing Orchestrator listening on %s", s.cfg.Address)
	go s.manager.Run(ctx)
	go s.writeToSTDOut(ctx)

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func (s *POServer) EnqueueDirective(
	stream grpc.ClientStreamingServer[pb.ProbingDirective, emptypb.Empty]) error {
	// Ensure only one active probe connection
	directiveCh := s.manager.GetDirectiveChannel()

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

func (s *POServer) EnqueueForwardingInfoElement(
	stream grpc.ClientStreamingServer[pb.ForwardingInfoElement, emptypb.Empty]) error {
	// Ensure only one active probe connection
	elementCh := s.manager.GetElementChannel()

	// Goroutine to send results back
	for {
		element, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-s.exitCtx.Done():
			return s.exitCtx.Err()
		case elementCh <- element:
			continue // element added to the streaming queue.
		}
	}
}

func (s *POServer) GetClusterStatus(
	ctx context.Context,
	a *emptypb.Empty) (*pb.ClusterStatus, error) {
	return s.manager.GetClusterStatus(), nil
}

func (s *POServer) RegisterProbingAgent(
	ctx context.Context,
	pa *pb.ProbingAgent) (*pb.RegisterProbingAgentResponse, error) {

	pa, err := s.manager.AddPA(pa)
	if err != nil {
		return &pb.RegisterProbingAgentResponse{
			Success:      false,
			Message:      err.Error(),
			ProbingAgent: nil,
		}, err
	}

	return &pb.RegisterProbingAgentResponse{
		Success:      true,
		Message:      "ok",
		ProbingAgent: pa,
	}, nil
}

func (s *POServer) RegisterProbingDirectiveGenerator(
	ctx context.Context,
	pdg *pb.ProbingDirectiveGenerator) (*pb.RegisterProbingDirectiveGeneratorResponse, error) {

	pdg, err := s.manager.AddPDG(pdg)
	if err != nil {
		return &pb.RegisterProbingDirectiveGeneratorResponse{
			Success:                   false,
			Message:                   err.Error(),
			ProbingDirectiveGenerator: nil,
		}, err
	}

	return &pb.RegisterProbingDirectiveGeneratorResponse{
		Success:                   true,
		Message:                   "ok",
		ProbingDirectiveGenerator: pdg,
	}, nil
}

func (s *POServer) UnregisterProbingAgent(
	ctx context.Context,
	r *pb.UnregistrationRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.manager.RemovePA(r.Id)
}

func (s *POServer) UnregisterProbingDirectiveGenerator(
	ctx context.Context,
	r *pb.UnregistrationRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, s.manager.RemovePDG(r.Id)
}

func (s *POServer) writeToSTDOut(ctx context.Context) {
	elementCh := s.manager.GetElementChannel()
	for {
		select {
		case <-ctx.Done():
			return
		case element := <-elementCh:
			bytes, err := json.Marshal(element)
			if err != nil {
				log.Fatalf("error occured on writing finfo to stdout: %v", err)
				continue
			}

			fmt.Printf("%v\n", string(bytes))
		}
	}
}
