package clients

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "iprl-demo/internal/gen/proto"
)

type OrchestratorClient struct {
	pb.ProbingOrchestratorInterfaceClient
}

func NewInsecureOrchestratorClient(address string) (*OrchestratorClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	client := &OrchestratorClient{
		pb.NewProbingOrchestratorInterfaceClient(conn),
	}

	return client, conn, nil
}
