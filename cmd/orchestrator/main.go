package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"iprl-demo/internal/components/orchestrator"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/servers"
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
		SoftwareVersion:             "1.0.0",
		InterfaceVersion:            "1.0.0",
		InterfaceAddr:               *address,
		NumRetries:                  uint32(*retries),
		DefaultGlobalProbingRateCap: uint32(*rate),
		DirectiveBufferLength:       uint32(*directiveBuff),
		ElementBufferLength:         uint32(*elementBuff),
	}

	probingOrchestrator := orchestrator.NewRealProbingOrchestrator(spec)
	probingOrchestratorServer := servers.NewOrchestratorServer(probingOrchestrator, spec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		cancel()
	}()

	probingOrchestratorServer.Run(ctx)
}
