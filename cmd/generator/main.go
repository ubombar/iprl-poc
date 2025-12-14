package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/generator"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"log"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
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

	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	// Signal handler
	g.Go(func() error {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		log.Printf("Received signal: %v", <-sigCh)
		cancel()
		return nil
	})

	g.Go(func() error {
		return probingGenerator.Run(ctx)
	})

	if err := g.Wait(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
