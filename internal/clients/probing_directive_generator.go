package clients

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "iprl-demo/internal/gen/proto"
)

type PDGClient struct {
	pb.PDGServiceClient
}

func NewInsecurePDGClient(address string) (*PDGClient, error) {
	poConn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := &PDGClient{
		pb.NewPDGServiceClient(poConn),
	}

	return client, nil
}
