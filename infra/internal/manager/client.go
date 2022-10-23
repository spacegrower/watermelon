package manager

import (
	"sync"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/wlog"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	clientLocker  sync.Mutex
	clientManager map[any]any

	proxyLocker  sync.RWMutex
	proxyManager map[string]string

	kvLocker sync.RWMutex
	kvstore  map[any]any
)

func init() {
	clientManager = make(map[any]any)
	proxyManager = make(map[string]string)
	kvstore = make(map[any]any)
}

func RegisterKV(key, val any) {
	kvLocker.Lock()
	kvstore[key] = val
	kvLocker.Unlock()
}

func ResolveKV(key any) any {
	kvLocker.RLocker().Lock()
	defer kvLocker.RLocker().Unlock()
	return kvstore[key]
}

func RegisterClient(key, client any) {
	clientLocker.Lock()
	clientManager[key] = client
	clientLocker.Unlock()
}

func ResolveClient(key any) any {
	clientLocker.Lock()
	defer clientLocker.Unlock()
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

func RegisterProxy(region, addr string) {
	proxyLocker.Lock()
	proxyManager[region] = addr
	proxyLocker.Unlock()
}

func ResolveProxy(region string) string {
	proxyLocker.RLocker().Lock()
	defer proxyLocker.RLocker().Unlock()
	return proxyManager[region]
}
