package main

import (
	"context"
	"flag"
	"iprl-demo/internal/api"
	"iprl-demo/internal/po"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var (
		address = flag.String("address", ":50050", "gRPC listen address")
		rate    = flag.Uint("rate", 1, "Probe generation rate per second")
	)
	flag.Parse()

	orchestrator := po.NewPOManager(&api.POConfig{
		SoftwareVersion:              "1.0.0",
		Address:                      *address,
		DefaultProbingRatePerSeconds: *rate,
	})

	poServer, err := po.NewPOServer(orchestrator, &po.POServerConfig{
		Address: *address,
	})
	if err != nil {
		log.Fatalf("failed to create the po server: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	poServer.Run(ctx)
}
