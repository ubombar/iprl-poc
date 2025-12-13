package clients

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "iprl-demo/internal/gen/proto"
)

type PAClient struct {
	pb.PAServiceClient
}

func NewInsecurePAClient(address string) (*PAClient, error) {
	poConn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := &PAClient{
		pb.NewPAServiceClient(poConn),
	}

	return client, nil
}
