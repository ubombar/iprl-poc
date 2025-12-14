package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/generator"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
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
		address   = flag.String("address", ":50049", "gRPC listen address")
		poAddr    = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		seed      = flag.Int64("seed", 42, "Random seed")
		rate      = flag.Uint("rate", 1, "Probe generation rate per second per agent")
		minTTL    = flag.Uint("min-ttl", 1, "Minimum TTL")
		maxTTL    = flag.Uint("max-ttl", 32, "Maximum TTL")
		retries   = flag.Uint("retries", 3, "Number of retries")
		protocols = flag.String("protocols", "icmp,udp", "Comma-separated list of protocols (icmp, tcp, udp, dccp, icmpv6)")
	)
	flag.Parse()

	// For now this is mock, in the future some of these values should be retrieved.
	spec := &pb.ProbingDirectiveGeneratorSpec{
		SoftwareVersion:             Version,
		InterfaceVersion:            Version,
		InterfaceAddr:               *address,
		OrchestratorAddress:         *poAddr,
		NumRetries:                  uint32(*retries),
		Protocols:                   util.ParseProtocols(*protocols),
		MinTtl:                      uint32(*minTTL),
		MaxTtl:                      uint32(*maxTTL),
		DefaultGlobalProbingRateCap: uint32(*rate),
		Seed:                        *seed,
	}

	probingGenerator := generator.NewGeneratorManager(spec)

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
		probingGenerator.Register(grpcServer)

		log.Printf("generator listening on %s", *address)
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
		return probingGenerator.Run(ctx)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		log.Fatalf("Error: %v", err)
	}

	log.Println("generator shutdown complete")
}
