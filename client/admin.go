package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"

	pba "github.com/SolmateDev/go-staker/proto/admin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type AdminConfiguration struct {
	Ssl            bool
	GrpcUrl        string
	GrpcServerName string
}

func CreateAdmin(ctx context.Context, config *AdminConfiguration) (pba.ValidatorClient, error) {
	var conn *grpc.ClientConn
	var err error
	if config.Ssl {
		conn, err = grpc.DialContext(ctx, config.GrpcUrl, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			ServerName: config.GrpcServerName,
		})))
	} else {
		conn, err = grpc.DialContext(ctx, config.GrpcUrl, grpc.WithInsecure())
	}
	if err != nil {
		return nil, err
	}

	return pba.NewValidatorClient(conn), nil
}

func LogStream(ctx context.Context, admin pba.ValidatorClient) error {
	stream, err := admin.GetLogStream(ctx, &pba.Empty{})
	if err != nil {
		return err
	}

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			err = nil
			break
		} else if err != nil {
			break
		}

		os.Stderr.WriteString(fmt.Sprintf("level=%d message=%s\n", msg.GetLevel(), msg.GetMessage()))
	}

	return err
}
