package orchestrator

import (
	"context"
	pb "iprl-demo/internal/gen/proto"
)

type ProbingOrchestrator interface {
	// This gets the spec object.
	GetSpec() *pb.ProbingOrchestratorSpec

	// This sets the status object
	SetStatus(s *pb.ProbingOrchestratorStatus)

	// Gets the directive queue/channel.
	GetProbingDirectiveChannel() chan *pb.ProbingDirective

	// Gets the element queue/channel.
	GetForwardingInfoElementChannel() chan *pb.ForwardingInfoElement

	// Registers the component
	RegisterComponent(req *pb.RegisterComponentRequest) (*pb.RegisterComponentResponse, error)

	// UnRegisters the component
	UnRegisterComponent(req *pb.UnRegisterComponentRequest) error

	// Used for the caller to wait for this to end execution.
	ExitContext() context.Context

	// Runs the logic in the same Go routine.
	Run(ctx context.Context)
}
