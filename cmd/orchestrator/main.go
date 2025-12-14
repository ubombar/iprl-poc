package main

import (
	"context"
	"flag"
	"net"
	"net/http"
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
		grpcAddr      = flag.String("grpc-addr", ":50050", "grpc listen address")
		httpAddr      = flag.String("http-addr", ":80", "http listen address")
		rate          = flag.Uint("rate", 1, "default global probing rate cap per agent")
		retries       = flag.Uint("retries", 3, "number of retries")
		directiveBuff = flag.Uint("directive-buffer", 10000, "directive buffer length")
		elementBuff   = flag.Uint("element-buffer", 10000, "element buffer length")
	)
	flag.Parse()

	spec := &pb.ProbingOrchestratorSpec{
		SoftwareVersion:             Version,
		InterfaceVersion:            Version,
		InterfaceAddr:               *grpcAddr,
		NumRetries:                  uint32(*retries),
		DefaultGlobalProbingRateCap: uint32(*rate),
		DirectiveBufferLength:       uint32(*directiveBuff),
		ElementBufferLength:         uint32(*elementBuff),
	}

	orchestratorManager := orchestrator.NewOrchestratorManager(spec)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// Create servers
	grpcServer := grpc.NewServer()
	orchestratorManager.Register(grpcServer)

	mux := http.NewServeMux()
	mux.HandleFunc("/stream", orchestratorManager.Stream)
	httpServer := &http.Server{
		Addr:    *httpAddr,
		Handler: mux,
	}

	// gRPC server
	g.Go(func() error {
		lis, err := net.Listen("tcp", *grpcAddr)
		if err != nil {
			return err
		}
		log.Printf("gRPC server listening on %s", *grpcAddr)
		return grpcServer.Serve(lis)
	})

	// HTTP server
	g.Go(func() error {
		log.Printf("HTTP server listening on %s", *httpAddr)
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	})

	// Graceful shutdown
	g.Go(func() error {
		<-ctx.Done()
		log.Println("Shutting down servers...")

		// Shutdown HTTP server with timeout
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		} else {
			log.Println("HTTP server stopped")
		}

		// Shutdown gRPC server
		grpcServer.GracefulStop()
		log.Println("gRPC server stopped")

		return nil
	})

	// Run orchestrator manager
	g.Go(func() error {
		return orchestratorManager.Run(ctx)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Fatalf("Error: %v", err)
	}

	log.Println("Shutdown complete")
}
