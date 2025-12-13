package pa

import (
	"context"
	pb "iprl-demo/internal/gen/proto"
)

type PA interface {
	GetSelf() *pb.ProbingAgent
	SetSelf(self *pb.ProbingAgent)
	SetClusterStatus(cs *pb.ClusterStatus)
	DirectiveChannel() chan<- *pb.ProbingDirective
	Done() <-chan struct{}
	Run(ctx context.Context)
}
