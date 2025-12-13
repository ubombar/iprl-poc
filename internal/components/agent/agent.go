package agent

import (
	"context"
	pb "iprl-demo/internal/gen/proto"
)

type ProbingAgent interface {
	// This gets the spec object.
	GetSpec() *pb.ProbingAgentSpec

	// This sets the status object
	SetStatus(s *pb.ProbingAgentStatus)

	// Used for the caller to wait for this to end execution.
	ExitContext() context.Context

	// Runs the logic in the same Go routine.
	Run(ctx context.Context)
}
