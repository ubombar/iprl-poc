package main

import (
	"context"
	"flag"

	"iprl-demo/internal/components/orchestrator"
	pb "iprl-demo/internal/gen/proto"
	"log"
	"os/signal"
	"syscall"
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
		GrpcAddress:                 *grpcAddr,
		HttpAddress:                 *httpAddr,
		NumRetries:                  uint32(*retries),
		DefaultGlobalProbingRateCap: uint32(*rate),
		DirectiveBufferLength:       uint32(*directiveBuff),
		ElementBufferLength:         uint32(*elementBuff),
	}

	orchestratorManager := orchestrator.NewOrchestratorManager(spec)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := orchestratorManager.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("error on orchestrator: %v", err)
	}

	log.Println("shutdown complete")
}
