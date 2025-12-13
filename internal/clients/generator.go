package clients

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "iprl-demo/internal/gen/proto"
)

type GeneratorClient struct {
	pb.ProbingDirectiveGeneratorInterfaceClient
}

func NewInsecureGeneratorClient(address string) (*GeneratorClient, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	client := &GeneratorClient{
		pb.NewProbingDirectiveGeneratorInterfaceClient(conn),
	}

	return client, conn, nil
}
