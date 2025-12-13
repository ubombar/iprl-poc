package generator

import (
	"context"
	pb "iprl-demo/internal/gen/proto"
)

type ProbingDirectiveGenerator interface {
	// This gets the spec object.
	GetSpec() *pb.ProbingDirectiveGeneratorSpec

	// This sets the status object
	SetStatus(s *pb.ProbingDirectiveGeneratorStatus)

	// Used for the caller to wait for this to end execution.
	ExitContext() context.Context

	// Runs the logic in the same Go routine.
	Run(ctx context.Context)
}
