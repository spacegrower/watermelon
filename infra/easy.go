package infra

import (
	"strings"

	clientv3 "go.etcd.io/etcd/client/v3"

	_ "github.com/spacegrower/watermelon/infra/balancer"
	"github.com/spacegrower/watermelon/infra/definition"
	ide "github.com/spacegrower/watermelon/infra/internal/definition"
	"github.com/spacegrower/watermelon/infra/internal/manager"
)

// RegisterETCDRegisterPrefixKey a function to change default register(etcd) prefix key
func RegisterETCDRegisterPrefixKey(prefix string) {
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	manager.RegisterKV(ide.ETCDPrefixKey{}, prefix)
}

// ResolveEtcdClient a function to register etcd client to watermelon global
func RegisterEtcdClient(etcdConfig clientv3.Config) error {
	client, err := clientv3.New(etcdConfig)
	if err != nil {
		return err
	}
	manager.RegisterEtcdClient(client)
	return nil
}

// ResolveEtcdClient a function to get registed etcd client
func ResolveEtcdClient() *clientv3.Client {
	client := manager.ResolveClient(definition.MANAGER_ETCD_REGISTER_KEY{})
	if client == nil {
		return nil
	}
	return client.(*clientv3.Client)
}

// RegisterRegionProxy set region's proxy endpoint
func RegisterRegionProxy(region, proxy string) {
	manager.RegisterProxy(region, proxy)
}

// ResolveProxy return region's proxy, if it exist
func ResolveProxy(region string) string {
	return manager.ResolveProxy(region)
}
