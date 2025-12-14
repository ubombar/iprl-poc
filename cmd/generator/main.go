package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/generator"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	_ "net/http/pprof" // enable cpu profiler.
)

// Set at build time
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	var (
		address          = flag.String("address", ":50049", "gRPC listen address")
		poAddr           = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		minTTL           = flag.Uint("min-ttl", 1, "Minimum TTL")
		maxTTL           = flag.Uint("max-ttl", 32, "Maximum TTL")
		retries          = flag.Uint("retries", 3, "Number of retries")
		protocols        = flag.String("protocols", "icmp,udp", "Comma-separated list of protocols (icmp, tcp, udp, dccp, icmpv6)")
		seed             = flag.Uint("seed", 42, "Seed for the mock generator")
		mockProberBuffer = flag.Uint("mock-buffer", 10000, "Buffer length of the mock generator")
	)
	flag.Parse()

	// For now this is mock, in the future some of these values should be retrieved.
	spec := &pb.ProbingDirectiveGeneratorSpec{
		SoftwareVersion:     Version,
		InterfaceVersion:    Version,
		GrpcAddress:         *address,
		OrchestratorAddress: *poAddr,
		NumRetries:          uint32(*retries),
		Protocols:           util.ParseProtocols(*protocols),
		MinTtl:              uint32(*minTTL),
		MaxTtl:              uint32(*maxTTL),
		Seed:                int64(*seed),
	}

	directiveGenerator, err := generator.NewMockDirectiveGenerator(int(*mockProberBuffer), int64(*seed), nil)
	if err != nil {
		log.Printf("cannot create mock directive generator: %v", err)
		return
	}

	probingGenerator := generator.NewGeneratorManager(spec, directiveGenerator)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	if err := probingGenerator.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("error on generator: %v", err)
	}

	log.Println("shutdown complete")
}
