package clients

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "iprl-demo/internal/gen/proto"
)

type POClient struct {
	pb.POServiceClient
}

func NewInsecurePOClient(address string) (*POClient, error) {
	poConn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	client := &POClient{
		pb.NewPOServiceClient(poConn),
	}

	return client, nil
}
