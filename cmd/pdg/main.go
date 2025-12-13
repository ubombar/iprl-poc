package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"iprl-demo/internal/api"
	"iprl-demo/internal/clients"
	"iprl-demo/internal/pdg"
)

func main() {
	var (
		address = flag.String("address", ":50049", "gRPC listen address")
		poAddr  = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		seed    = flag.Int64("seed", 42, "Random seed")
		rate    = flag.Uint("rate", 1, "Probe generation rate per second")
		minTTL  = flag.Uint("min-ttl", 1, "Minimum TTL")
		maxTTL  = flag.Uint("max-ttl", 32, "Maximum TTL")
	)
	flag.Parse()

	poClient, err := clients.NewInsecurePOClient(*poAddr)
	if err != nil {
		log.Fatalf("failed to connect to the orchestrator: %v", err)
	}

	mockGenerator := pdg.NewMockGenerator(poClient, &api.MockPDGConfig{
		SoftwareVersion:              "1.0.0",
		DefaultProbingRatePerSeconds: *rate,
		Seed:                         *seed,
		MinTTL:                       uint8(*minTTL),
		MaxTTL:                       uint8(*maxTTL),
		Address:                      *address,
	})

	pdgServer, err := pdg.NewServer(mockGenerator, poClient, &pdg.PDGServerConfig{
		Address: *address,
	})
	if err != nil {
		log.Fatalf("failed to create the pdg server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	pdgServer.Run(ctx)
}
