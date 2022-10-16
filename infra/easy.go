package infra

import (
	clientv3 "go.etcd.io/etcd/client/v3"

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
