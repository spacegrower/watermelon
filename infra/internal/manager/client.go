package manager

import (
	"sync"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/wlog"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	locker        sync.Mutex
	clientManager map[any]any
)

func init() {
	clientManager = make(map[any]any)
}

func RegisterClient(key, client any) {
	locker.Lock()
	clientManager[key] = client
	locker.Unlock()
}

func ResolveClient(key any) any {
	locker.Lock()
	defer locker.Unlock()
	return clientManager[key]
}

func RegisterEtcdClient(c *clientv3.Client) {
	RegisterClient(definition.MANAGER_ETCD_REGISTER_KEY{}, c)
}

func MustResolveEtcdClient() *clientv3.Client {
	client := ResolveClient(definition.MANAGER_ETCD_REGISTER_KEY{})
	if client == nil {
		wlog.Panic("failed to resolve etcd client, please check already registered etcd-client")
	}
	return client.(*clientv3.Client)
}
