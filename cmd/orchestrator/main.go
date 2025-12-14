package main

import (
	"context"
	"flag"
	"net"
	"time"

	"iprl-demo/internal/components/orchestrator"
	pb "iprl-demo/internal/gen/proto"
	"log"
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
		rate          = flag.Uint("rate", 1, "Default global probing rate cap per agent")
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

	// Create listener first
	lis, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	orchestratorManager.Register(grpcServer)

	g, ctx := errgroup.WithContext(ctx)

	// gRPC server
	g.Go(func() error {
		log.Printf("Orchestrator listening on %s", *address)
		return grpcServer.Serve(lis)
	})

	// Graceful shutdown - this is the key part
	g.Go(func() error {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")

		// Use a timeout for graceful stop
		stopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			log.Println("gRPC server stopped gracefully")
		case <-time.After(1 * time.Second): // TODO: The graceful shutdown never happens this is a bug!
			log.Println("gRPC server force stopping...")
			grpcServer.Stop()
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

	log.Println("orchestrator shutdown complete")
}
