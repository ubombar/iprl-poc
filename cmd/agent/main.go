package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/agent"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"

	"google.golang.org/protobuf/proto"

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
		address          = flag.String("address", ":50051", "gRPC listen address")
		poAddr           = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		dbufflen         = flag.Uint("buffer-length", 10000, "Directive buffer length")
		vpip             = flag.String("vp-ip", "1.1.1.1", "Vantage point IP address")
		vpName           = flag.String("vp-name", "vp-1", "Vantage point name")
		vpASN            = flag.Uint("vp-asn", 0, "Vantage point ASN")
		provider         = flag.String("vp-provider", "unknown", "Vantage point provider (gcp, aws, edgenet, unknown)")
		seed             = flag.Uint("seed", 42, "Seed for the mock prober")
		mockProberBuffer = flag.Uint("mock-buffer", 10000, "Buffer length of the mock prober")
	)
	flag.Parse()

	// For now this is mock, in the future some of these values should be retrieved.
	spec := &pb.ProbingAgentSpec{
		SoftwareVersion:  Version,
		InterfaceVersion: Version,
		GrpcAddress:      *address,
		VantagePoint: &pb.VantagePoint{
			Name:          *vpName,
			PublicAddress: net.ParseIP(*vpip).To4(), // or .To16() for IPv6
			Asn:           proto.Uint32(uint32(*vpASN)),
			Provider:      util.ParseProvider(*provider),
		},
		DirectiveBufferLength: uint32(*dbufflen),
		OrchestratorAddress:   *poAddr,
		NumRetries:            uint32(0), // for now it is hardcoded.
	}

	prober, err := agent.NewMockProber(int(*mockProberBuffer), int64(*seed), nil)
	if err != nil {
		log.Printf("cannot create mock prober: %v", err)
		return
	}

	probingAgent := agent.NewAgentManager(spec, prober)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		http.ListenAndServe("localhost:6060", nil)
	}()

	if err := probingAgent.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("error on agent: %v", err)
	}

	log.Println("shutdown complete")
}
