// Package components defines the core interfaces for the IPRL probing system.
// It provides abstractions for the three main component types: Probing Agents (PA),
// Probing Directive Generators (PDG), and the Probing Orchestrator (PO).
package components

import (
	"context"

	pb "iprl-demo/internal/gen/proto"
)

// Runner represents a component that can be started and run with a context.
// The Run method should block until the context is cancelled or an error occurs.
type Runner interface {
	Run(ctx context.Context) error
}

// MetaManager provides generic accessors for a component's specification and status.
// Spec defines the component's configuration (typically immutable after registration).
// Status represents the component's current runtime state (mutable, updated by orchestrator).
type MetaManager[Spec, Status any] interface {
	// GetSpec returns the component's specification.
	GetSpec() Spec

	// SetSpec updates the component's specification.
	SetSpec(Spec)

	// GetStatus returns the component's current status.
	GetStatus() Status

	// SetStatus updates the component's status, typically called when
	// the orchestrator pushes new configuration (e.g., rate limits).
	SetStatus(Status)
}

// AgentManager defines the interface for a Probing Agent (PA).
// A Probing Agent receives probing directives from the orchestrator,
// executes network probes, and returns forwarding information elements.
type AgentManager interface {
	Runner

	MetaManager[*pb.ProbingAgentSpec, *pb.ProbingAgentStatus]
}

// GeneratorManager defines the interface for a Probing Directive Generator (PDG).
// A PDG generates probing directives based on its configuration and streams
// them to the orchestrator for distribution to agents.
type GeneratorManager interface {
	Runner

	MetaManager[*pb.ProbingDirectiveGeneratorSpec, *pb.ProbingDirectiveGeneratorStatus]
}

// OrchestratorManager defines the interface for the Probing Orchestrator (PO).
// The orchestrator is the central coordinator that:
//   - Manages registration and lifecycle of agents and generators
//   - Routes probing directives from generators to appropriate agents
//   - Collects forwarding information elements from agents
//   - Maintains and distributes cluster status to all components
type OrchestratorManager interface {
	Runner

	MetaManager[*pb.ProbingOrchestratorSpec, *pb.ProbingOrchestratorStatus]

	// EnqueueDirective adds a probing directive to the distribution queue.
	// Called by generators to submit directives for routing to agents.
	EnqueueDirective(ctx context.Context, directive *pb.ProbingDirective)

	// EnqueueElement adds a forwarding information element to the collection queue.
	// Called by agents to submit probe results.
	EnqueueElement(ctx context.Context, element *pb.ForwardingInfoElement)
}
