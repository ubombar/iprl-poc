package clients

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "iprl-demo/internal/gen/proto"
)

type AgentClient struct {
	pb.ProbingAgentInterfaceClient
}

func NewInsecureAgentClient(address string) (*AgentClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	client := &AgentClient{
		pb.NewProbingAgentInterfaceClient(conn),
	}

	return client, conn, nil
}
