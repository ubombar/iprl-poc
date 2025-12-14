package main

import (
	"context"
	"flag"

	"iprl-demo/internal/components/orchestrator"
	pb "iprl-demo/internal/gen/proto"
	"log"
	"net"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

// Set at build time
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	var (
		address       = flag.String("address", ":50050", "gRPC listen address")
		rate          = flag.Uint("rate", 1000, "Default global probing rate cap per agent")
		retries       = flag.Uint("retries", 3, "Number of retries")
		directiveBuff = flag.Uint("directive-buffer", 10000, "Directive buffer length")
		elementBuff   = flag.Uint("element-buffer", 10000, "Element buffer length")
	)
	flag.Parse()

	spec := &pb.ProbingOrchestratorSpec{
		SoftwareVersion:             Version,
		InterfaceVersion:            Version,
		InterfaceAddr:               *address,
		NumRetries:                  uint32(*retries),
		DefaultGlobalProbingRateCap: uint32(*rate),
		DirectiveBufferLength:       uint32(*directiveBuff),
		ElementBufferLength:         uint32(*elementBuff),
	}

	orchestratorManager := orchestrator.NewOrchestratorManager(spec)

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
		orchestratorManager.Register(grpcServer)

		log.Printf("Orchestrator listening on %s", *address)
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
		return orchestratorManager.Run(ctx)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Fatalf("Error: %v", err)
	}

	log.Println("Orchestrator shutdown complete")
}
