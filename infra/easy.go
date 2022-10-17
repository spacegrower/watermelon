package infra

import (
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/internal/manager"
)

func RegisterEtcdClient(etcdConfig clientv3.Config) error {
	client, err := clientv3.New(etcdConfig)
	if err != nil {
		return err
	}
	manager.RegisterEtcdClient(client)
	return nil
}

func RegisterRegionProxy(region, proxy string) {
	manager.RegisterProxy(region, proxy)
}

type infra struct {
	NewServer     Server
	NewClientConn ClientConn
}

type Server func(register func(srv *grpc.Server), opts ...Option) *server
type ClientConn func(serviceName string, opts ...ClientOptions) (grpc.ClientConnInterface, error)

func NewServer() Server {
	return Server(newServer)
}

func NewClientConn() ClientConn {
	return ClientConn(newClientConn)
}
