package main

import (
	"context"
	"flag"
	"iprl-demo/internal/api"
	"iprl-demo/internal/clients"
	"iprl-demo/internal/pa"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	var (
		address  = flag.String("address", ":50051", "gRPC listen address")
		poAddr   = flag.String("po-addr", "localhost:50050", "Probing Orchestrator address")
		seed     = flag.Int64("seed", 42, "Random seed")
		rate     = flag.Uint("rate", 1, "Probe generation rate per second")
		dbufflen = flag.Uint("buffer-length", 1000, "Directive buffer length")
		vpip     = flag.String("vp-ip", "127.0.0.1", "Vantage point IP address")
		vpid     = flag.String("vp-id", "vp-1", "Vantage point ID")
		asn      = flag.Uint("vp-asn", 0, "Vantage point ASN")
		vploc    = flag.String("vp-location", "", "Vantage point location")
		provider = flag.String("vp-provider", "unknown", "Vantage point provider (gcp, aws, edgenet, unknown)")
	)
	flag.Parse()

	poClient, err := clients.NewInsecurePOClient(*poAddr)
	if err != nil {
		log.Fatalf("failed to connect to the orchestrator: %v", err)
	}

	mockAgent := pa.NewMockAgent(poClient, &api.MockPAConfig{
		SoftwareVersion:             "1.0.0",
		Seed:                        *seed,
		Address:                     *address,
		DefaultProbingRatePerSecond: uint32(*rate),
		DirectiveBufferLength:       uint(*dbufflen),
		VantagePointIP:              net.ParseIP(*vpip),
		VantagePointID:              *vpid,
		VantagePointASN:             uint32(*asn),
		VantagePointLocation:        *vploc,
		VantagePointProvider:        *provider,
	})

	pdgServer, err := pa.NewPAServer(mockAgent, poClient, &pa.PAServerConfig{
		Address: *address,
	})
	if err != nil {
		log.Fatalf("failed to create the pa server: %v", err)
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
