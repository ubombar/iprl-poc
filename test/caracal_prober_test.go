package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"

	"iprl-demo/internal/components/agent"
	pb "iprl-demo/internal/gen/proto"

	"google.golang.org/protobuf/proto"
)

func main() {
	caracalPath := flag.String("caracal", "test/caracal.sh", "Path to caracal binary")
	flag.Parse()

	prober, err := agent.NewCaracalProber(1000, *caracalPath, 30*time.Second, nil)
	if err != nil {
		log.Fatalf("Failed to create prober: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Start prober
	go func() {
		if err := prober.Run(ctx); err != nil {
			log.Printf("Prober error: %v", err)
		}
	}()

	// Print results
	go func() {
		for element := range prober.PullChannel() {
			data, _ := json.MarshalIndent(element, "", "  ")
			log.Printf("Received element:\n%s", data)
		}
	}()

	// Wait for caracal to start
	time.Sleep(1 * time.Second)

	// Send test directives to 1.1.1.1
	ip := net.ParseIP("1.1.1.1").To4()

	for ttl := uint32(1); ttl <= 5; ttl++ {
		directive := &pb.ProbingDirective{
			VantagePointName:   "test",
			DestinationAddress: ip,
			IpVersion:          pb.IPVersion_IP_VERSION_4,
			Protocol:           pb.Protocol_PROTOCOL_ICMP,
			NearTtl:            ttl,
			HeaderParameters: &pb.HeaderParameters{
				SourcePort:      proto.Uint32(24000),
				DestinationPort: proto.Uint32(0),
			},
		}

		log.Printf("Sending directive: dst=1.1.1.1, ttl=%d", ttl)
		prober.PushChannel() <- directive

		time.Sleep(100 * time.Millisecond)
	}

	// Wait for responses
	log.Println("Waiting for responses (Ctrl+C to exit)...")
	<-ctx.Done()

	prober.Close()
	log.Println("Done")
}
