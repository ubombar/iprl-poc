package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/generator"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/servers"
	"iprl-demo/internal/util"
	"os"
	"os/signal"
	"syscall"
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
		SoftwareVersion:             "1.0.0",
		InterfaceVersion:            "1.0.0",
		InterfaceAddr:               *address,
		OrchestratorAddress:         *poAddr,
		NumRetries:                  uint32(*retries),
		Protocols:                   util.ParseProtocols(*protocols),
		MinTtl:                      uint32(*minTTL),
		MaxTtl:                      uint32(*maxTTL),
		DefaultGlobalProbingRateCap: uint32(*rate),
		Seed:                        *seed,
	}

	probingDirectiveGenerator := generator.NewMockProbingDirectiveGenerator(spec)
	probingDirectiveGeneratorServer := servers.NewGeneratorServer(probingDirectiveGenerator, spec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	probingDirectiveGeneratorServer.Run(ctx)
}
