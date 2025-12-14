package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"iprl-demo/internal/components"
	pb "iprl-demo/internal/gen/proto"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// CaracalProber wraps the caracal probing tool.
type CaracalProber struct {
	inCh  chan *pb.ProbingDirective
	outCh chan *pb.ForwardingInfoElement

	closed  atomic.Bool
	closeCh chan struct{}

	agentManager components.AgentManager

	// Caracal process
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	// Pending probes waiting for near_ttl and near_ttl+1 responses
	pendingMu sync.Mutex
	pending   map[string]*pendingProbe

	// Configuration
	caracalPath     string
	matchTimeout    time.Duration
	cleanupInterval time.Duration
}

// probeKey uniquely identifies a probe by its 5-tuple + ttl
type probeKey struct {
	dstAddr  string
	srcPort  uint32
	dstPort  uint32
	protocol pb.Protocol
	ttl      uint32
}

// pendingProbe tracks responses for near_ttl and near_ttl+1
type pendingProbe struct {
	directive    *pb.ProbingDirective
	createdAt    time.Time
	nearResponse *caracalResponse
	farResponse  *caracalResponse
}

// caracalResponse represents a parsed caracal output line
type caracalResponse struct {
	captureTimestamp int64
	probeProtocol    int
	probeSrcAddr     string
	probeDstAddr     string
	probeSrcPort     int
	probeDstPort     int
	probeTTL         int
	quotedTTL        int
	replySrcAddr     string
	replyProtocol    int
	replyICMPType    int
	replyICMPCode    int
	replyTTL         int
	replySize        int
	replyMPLSLabels  string
	rtt              int64 // in microseconds
	round            int
}

var _ components.Prober = (*CaracalProber)(nil)

// NewCaracalProber creates a new caracal prober.
func NewCaracalProber(buffer int, caracalPath string, matchTimeout time.Duration, agentManager components.AgentManager) (*CaracalProber, error) {
	if matchTimeout == 0 {
		matchTimeout = 30 * time.Second
	}

	return &CaracalProber{
		inCh:            make(chan *pb.ProbingDirective, buffer),
		outCh:           make(chan *pb.ForwardingInfoElement, buffer),
		closeCh:         make(chan struct{}),
		agentManager:    agentManager,
		pending:         make(map[string]*pendingProbe),
		caracalPath:     caracalPath,
		matchTimeout:    matchTimeout,
		cleanupInterval: 5 * time.Second,
	}, nil
}

// PushChannel implements Pusher.
func (c *CaracalProber) PushChannel() chan<- *pb.ProbingDirective {
	return c.inCh
}

// PullChannel implements Puller.
func (c *CaracalProber) PullChannel() <-chan *pb.ForwardingInfoElement {
	return c.outCh
}

// Register saves the reference of the manager.
func (c *CaracalProber) Register(g components.AgentManager) {
	c.agentManager = g
}

// Run implements Runner.
func (c *CaracalProber) Run(ctx context.Context) error {
	// Start caracal process
	if err := c.startCaracal(ctx); err != nil {
		return fmt.Errorf("failed to start caracal: %w", err)
	}
	defer c.stopCaracal()

	// Start goroutines
	var wg sync.WaitGroup

	// Read caracal output
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.readCaracalOutput(ctx)
	}()

	// Cleanup expired pending probes
	wg.Add(1)
	go func() {
		defer wg.Done()
		c.cleanupExpiredProbes(ctx)
	}()

	// Process incoming directives
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return nil

		case <-c.closeCh:
			wg.Wait()
			return nil

		case directive, ok := <-c.inCh:
			if !ok {
				wg.Wait()
				return nil
			}

			if err := c.sendDirective(directive); err != nil {
				// Log error but continue
				continue
			}
		}
	}
}

// Close implements io.Closer.
func (c *CaracalProber) Close() error {
	if c.closed.Swap(true) {
		return nil
	}

	close(c.closeCh)
	c.stopCaracal()
	close(c.outCh)

	return nil
}

// startCaracal starts the caracal process.
func (c *CaracalProber) startCaracal(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.caracalPath)

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start caracal: %w", err)
	}

	return nil
}

// stopCaracal stops the caracal process.
func (c *CaracalProber) stopCaracal() {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.stdout != nil {
		c.stdout.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
}

// sendDirective sends a directive to caracal (two lines: near_ttl and near_ttl+1).
func (c *CaracalProber) sendDirective(directive *pb.ProbingDirective) error {
	dstAddr := net.IP(directive.DestinationAddress).String()
	protocol := protocolToCaracal(directive.Protocol)

	var srcPort, dstPort uint32
	if directive.HeaderParameters != nil {
		if directive.HeaderParameters.SourcePort != nil {
			srcPort = *directive.HeaderParameters.SourcePort
		}
		if directive.HeaderParameters.DestinationPort != nil {
			dstPort = *directive.HeaderParameters.DestinationPort
		}
	}

	nearTTL := directive.NearTtl
	farTTL := nearTTL + 1

	// Register pending probe
	key := c.makeBaseKey(dstAddr, srcPort, dstPort, directive.Protocol)
	c.pendingMu.Lock()
	c.pending[key] = &pendingProbe{
		directive: directive,
		createdAt: time.Now(),
	}
	c.pendingMu.Unlock()

	// Send near_ttl line
	nearLine := fmt.Sprintf("%s,%d,%d,%d,%s\n", dstAddr, srcPort, dstPort, nearTTL, protocol)
	if _, err := c.stdin.Write([]byte(nearLine)); err != nil {
		return fmt.Errorf("failed to write near_ttl line: %w", err)
	}

	// Send far_ttl line (near_ttl + 1)
	farLine := fmt.Sprintf("%s,%d,%d,%d,%s\n", dstAddr, srcPort, dstPort, farTTL, protocol)
	if _, err := c.stdin.Write([]byte(farLine)); err != nil {
		return fmt.Errorf("failed to write far_ttl line: %w", err)
	}

	return nil
}

// readCaracalOutput reads and parses caracal output.
func (c *CaracalProber) readCaracalOutput(ctx context.Context) {
	scanner := bufio.NewScanner(c.stdout)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case <-c.closeCh:
			return
		default:
		}

		line := scanner.Text()

		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || len(strings.TrimSpace(line)) == 0 {
			continue
		}

		response, err := parseCaracalResponse(line)
		if err != nil {
			continue
		}

		c.handleResponse(ctx, response)
	}
}

// parseCaracalResponse parses a caracal output line.
func parseCaracalResponse(line string) (*caracalResponse, error) {
	fields := strings.Split(line, ",")
	if len(fields) < 17 {
		return nil, fmt.Errorf("invalid line: expected 17 fields, got %d", len(fields))
	}

	captureTimestamp, _ := strconv.ParseInt(fields[0], 10, 64)
	probeProtocol, _ := strconv.Atoi(fields[1])
	probeSrcPort, _ := strconv.Atoi(fields[4])
	probeDstPort, _ := strconv.Atoi(fields[5])
	probeTTL, _ := strconv.Atoi(fields[6])
	quotedTTL, _ := strconv.Atoi(fields[7])
	replyProtocol, _ := strconv.Atoi(fields[9])
	replyICMPType, _ := strconv.Atoi(fields[10])
	replyICMPCode, _ := strconv.Atoi(fields[11])
	replyTTL, _ := strconv.Atoi(fields[12])
	replySize, _ := strconv.Atoi(fields[13])
	rtt, _ := strconv.ParseInt(fields[15], 10, 64)
	round, _ := strconv.Atoi(fields[16])

	return &caracalResponse{
		captureTimestamp: captureTimestamp,
		probeProtocol:    probeProtocol,
		probeSrcAddr:     cleanIPv4(fields[2]),
		probeDstAddr:     cleanIPv4(fields[3]),
		probeSrcPort:     probeSrcPort,
		probeDstPort:     probeDstPort,
		probeTTL:         probeTTL,
		quotedTTL:        quotedTTL,
		replySrcAddr:     cleanIPv4(fields[8]),
		replyProtocol:    replyProtocol,
		replyICMPType:    replyICMPType,
		replyICMPCode:    replyICMPCode,
		replyTTL:         replyTTL,
		replySize:        replySize,
		replyMPLSLabels:  fields[14],
		rtt:              rtt,
		round:            round,
	}, nil
}

// cleanIPv4 removes IPv6 prefix from IPv4-mapped addresses.
func cleanIPv4(addr string) string {
	// Handle ::ffff:10.17.0.137 format
	if strings.HasPrefix(addr, "::ffff:") {
		return strings.TrimPrefix(addr, "::ffff:")
	}
	return addr
}

// handleResponse processes a caracal response and matches with pending probes.
func (c *CaracalProber) handleResponse(ctx context.Context, response *caracalResponse) {
	protocol := caracalToProtocol(response.probeProtocol)
	key := c.makeBaseKey(response.probeDstAddr, uint32(response.probeSrcPort), uint32(response.probeDstPort), protocol)

	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()

	pending, ok := c.pending[key]
	if !ok {
		return
	}

	nearTTL := pending.directive.NearTtl
	farTTL := nearTTL + 1

	// Match response to near or far TTL
	if uint32(response.probeTTL) == nearTTL {
		pending.nearResponse = response
	} else if uint32(response.probeTTL) == farTTL {
		pending.farResponse = response
	}

	// Check if we have both responses
	if pending.nearResponse != nil && pending.farResponse != nil {
		element := c.buildForwardingInfoElement(pending)
		delete(c.pending, key)

		select {
		case c.outCh <- element:
		case <-ctx.Done():
		case <-c.closeCh:
		}
	}
}

// buildForwardingInfoElement creates a ForwardingInfoElement from matched responses.
func (c *CaracalProber) buildForwardingInfoElement(pending *pendingProbe) *pb.ForwardingInfoElement {
	directive := pending.directive
	nearResp := pending.nearResponse
	farResp := pending.farResponse

	// Get vantage point from agent manager
	var vantagePoint *pb.VantagePoint
	if c.agentManager != nil {
		spec := c.agentManager.GetSpec()
		if spec != nil {
			vantagePoint = spec.VantagePoint
		}
	}

	// Parse addresses
	nearReplyAddr := net.ParseIP(nearResp.replySrcAddr)
	farReplyAddr := net.ParseIP(farResp.replySrcAddr)
	srcAddr := net.ParseIP(nearResp.probeSrcAddr)

	// Calculate timestamps
	nearSentAt := time.Unix(nearResp.captureTimestamp, 0).Add(-time.Duration(nearResp.rtt) * time.Microsecond)
	nearReceivedAt := time.Unix(nearResp.captureTimestamp, 0)
	farSentAt := time.Unix(farResp.captureTimestamp, 0).Add(-time.Duration(farResp.rtt) * time.Microsecond)
	farReceivedAt := time.Unix(farResp.captureTimestamp, 0)

	// Parse MPLS labels
	mpls := parseMPLSLabels(nearResp.replyMPLSLabels)

	return &pb.ForwardingInfoElement{
		VantagePoint:     vantagePoint,
		NearTtl:          directive.NearTtl,
		NearReplyAddress: nearReplyAddr.To4(),
		FarReplyAddress:  farReplyAddr.To4(),
		DestinationAddr:  directive.DestinationAddress,
		SourceAddr:       srcAddr.To4(),
		Mpls:             mpls,
		PacketSize:       int32(nearResp.replySize),
		NearRtt: &pb.RTTInfo{
			SentAt:     timestamppb.New(nearSentAt),
			ReceivedAt: timestamppb.New(nearReceivedAt),
		},
		FarRtt: &pb.RTTInfo{
			SentAt:     timestamppb.New(farSentAt),
			ReceivedAt: timestamppb.New(farReceivedAt),
		},
		PacketsSent:           2,
		PacketsReceived:       2,
		ChangeType:            pb.ChangeType_ANNOUNCED,
		JustificationType:     pb.JustificationType_OBSERVED,
		ConstructionTimestamp: timestamppb.Now(),
	}
}

// parseMPLSLabels parses MPLS labels from caracal format.
// Expected format: "[label1,tc1,s1,ttl1|label2,tc2,s2,ttl2]" or "[]"
func parseMPLSLabels(s string) *pb.MPLSInfo {
	s = strings.Trim(s, "[]")
	if len(s) == 0 {
		return nil
	}

	var labels []*pb.MPLSLabel
	for _, labelStr := range strings.Split(s, "|") {
		parts := strings.Split(labelStr, ",")
		if len(parts) < 4 {
			continue
		}

		label, _ := strconv.ParseUint(parts[0], 10, 32)
		tc, _ := strconv.ParseUint(parts[1], 10, 32)
		sFlag := parts[2] == "1"
		ttl, _ := strconv.ParseUint(parts[3], 10, 32)

		labels = append(labels, &pb.MPLSLabel{
			Label: uint32(label),
			Tc:    uint32(tc),
			S:     sFlag,
			Ttl:   uint32(ttl),
		})
	}

	if len(labels) == 0 {
		return nil
	}

	return &pb.MPLSInfo{Labels: labels}
}

// cleanupExpiredProbes removes pending probes that have exceeded the match timeout.
func (c *CaracalProber) cleanupExpiredProbes(ctx context.Context) {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.closeCh:
			return
		case <-ticker.C:
			c.pendingMu.Lock()
			now := time.Now()
			for key, pending := range c.pending {
				if now.Sub(pending.createdAt) > c.matchTimeout {
					delete(c.pending, key)
				}
			}
			c.pendingMu.Unlock()
		}
	}
}

// makeBaseKey creates a key for matching probes (without TTL).
func (c *CaracalProber) makeBaseKey(dstAddr string, srcPort, dstPort uint32, protocol pb.Protocol) string {
	return fmt.Sprintf("%s:%d:%d:%d", dstAddr, srcPort, dstPort, protocol)
}

// protocolToCaracal converts protobuf Protocol to caracal format.
func protocolToCaracal(p pb.Protocol) string {
	switch p {
	case pb.Protocol_PROTOCOL_ICMP:
		return "icmp"
	case pb.Protocol_PROTOCOL_TCP:
		return "tcp"
	case pb.Protocol_PROTOCOL_UDP:
		return "udp"
	case pb.Protocol_PROTOCOL_ICMPV6:
		return "icmp6"
	default:
		return "icmp"
	}
}

// caracalToProtocol converts caracal protocol number to protobuf Protocol.
func caracalToProtocol(p int) pb.Protocol {
	switch p {
	case 1:
		return pb.Protocol_PROTOCOL_ICMP
	case 6:
		return pb.Protocol_PROTOCOL_TCP
	case 17:
		return pb.Protocol_PROTOCOL_UDP
	case 58:
		return pb.Protocol_PROTOCOL_ICMPV6
	default:
		return pb.Protocol_PROTOCOL_UNKNOWN
	}
}
