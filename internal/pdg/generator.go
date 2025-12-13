package pdg

import (
	"context"
	pb "iprl-demo/internal/gen/proto"
)

type PDG interface {
	GetSelf() *pb.ProbingDirectiveGenerator
	SetSelf(self *pb.ProbingDirectiveGenerator)
	SetClusterStatus(cs *pb.ClusterStatus)
	Done() <-chan struct{}
	Run(ctx context.Context)
}
