package main

import (
	"context"
	"flag"
	"iprl-demo/internal/components/agent"
	pb "iprl-demo/internal/gen/proto"
	"iprl-demo/internal/util"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"
)

// Set at build time
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	var (
		address  = flag.String("address", ":50051", "gRPC listen address")
		poAddr   = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		dbufflen = flag.Uint("buffer-length", 1000, "Directive buffer length")
		vpip     = flag.String("vp-ip", "1.1.1.1", "Vantage point IP address")
		vpName   = flag.String("vp-name", "vp-1", "Vantage point name")
		vpASN    = flag.Uint("vp-asn", 0, "Vantage point ASN")
		provider = flag.String("vp-provider", "unknown", "Vantage point provider (gcp, aws, edgenet, unknown)")
		// retries  = flag.Uint("retries", 0, "Number of retries")
	)
	flag.Parse()

	// For now this is mock, in the future some of these values should be retrieved.
	spec := &pb.ProbingAgentSpec{
		SoftwareVersion:  Version,
		InterfaceVersion: Version,
		InterfaceAddr:    *address,
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

	probingAgent := agent.NewAgentManager(spec)

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
		return probingAgent.Run(ctx)
	})

	if err := g.Wait(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}
