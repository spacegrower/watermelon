package infra

import (
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	resolver "github.com/spacegrower/watermelon/infra/resolver/etcd"
)

func RegisterEtcdClient(etcdConfig clientv3.Config) error {
	client, err := clientv3.New(etcdConfig)
	if err != nil {
		return err
	}
	manager.RegisterEtcdClient(client)
	return nil
}

// NewDefaultServer is a function to create grpc server with registry
func NewDefaultServer(grpcServiceRegister func(srv *grpc.Server), opts ...Option) *server {
	rty := etcd.MustSetupEtcdRegister()
	opts = append(opts, WithServiceRegister(rty))
	return NewServer(grpcServiceRegister, opts...)
}

// MustSetupDefaultClientConn is a function to create grcp client connection
func MustSetupDefaultClientConn(serviceName string, opts ...ClientOptions) grpc.ClientConnInterface {
	opts = append(opts, ClientWithServiceResolver(resolver.MustSetupEtcdResolver()))
	cc, err := NewClientConn(serviceName, opts...)
	if err != nil {
		panic(err)
	}
	return cc
}
