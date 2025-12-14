package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/agent"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"log"
	"net"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// Set at build time
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	var (
		address  = flag.String("address", ":50051", "gRPC listen address")
		poAddr   = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		dbufflen = flag.Uint("buffer-length", 1000, "Directive buffer length")
		vpip     = flag.String("vp-ip", "1.1.1.1", "Vantage point IP address")
		vpName   = flag.String("vp-name", "vp-1", "Vantage point name")
		vpASN    = flag.Uint("vp-asn", 0, "Vantage point ASN")
		provider = flag.String("vp-provider", "unknown", "Vantage point provider (gcp, aws, edgenet, unknown)")
		// retries  = flag.Uint("retries", 0, "Number of retries")
		seed             = flag.Uint("seed", 42, "Seed for the mock prober")
		mockProberBuffer = flag.Uint("mock-buffer", 100, "Buffer length of the mock prober")
	)
	flag.Parse()

	// For now this is mock, in the future some of these values should be retrieved.
	spec := &pb.ProbingAgentSpec{
		SoftwareVersion:  Version,
		InterfaceVersion: Version,
		InterfaceAddr:    *address,
		VantagePoint: &pb.VantagePoint{
			Name:          *vpName,
			PublicAddress: net.ParseIP(*vpip).To4(), // or .To16() for IPv6
			Asn:           proto.Uint32(uint32(*vpASN)),
			Provider:      util.ParseProvider(*provider),
		},
		DirectiveBufferLength: uint32(*dbufflen),
		OrchestratorAddress:   *poAddr,
		NumRetries:            uint32(0), // for now it is hardcoded.
	}

	prober, err := agent.NewMockProber(int(*mockProberBuffer), int64(*seed), nil)
	if err != nil {
		log.Printf("cannot create mock prober: %v", err)
		return
	}

	probingAgent := agent.NewAgentManager(spec, prober)

	// Setup context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// gRPC server
	var grpcServer *grpc.Server
	g.Go(func() error {
		lis, err := net.Listen("tcp", *address)
		if err != nil {
			return err
		}

		grpcServer = grpc.NewServer()
		probingAgent.Register(grpcServer)

		log.Printf("agent listening on %s", *address)
		return grpcServer.Serve(lis)
	})

	// Graceful shutdown
	g.Go(func() error {
		<-ctx.Done()
		if grpcServer != nil {
			log.Println("Shutting down gRPC server...")
			grpcServer.GracefulStop()
		}
		return nil
	})

	// Run orchestrator manager
	g.Go(func() error {
		return probingAgent.Run(ctx)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Fatalf("Error: %v", err)
	}

	log.Println("agent shutdown complete")
}
