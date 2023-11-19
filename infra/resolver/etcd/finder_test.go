package etcd_test

import (
	"net/url"
	"testing"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc/resolver"

	"github.com/spacegrower/watermelon"
	etcdregister "github.com/spacegrower/watermelon/infra/register/etcd"
	"github.com/spacegrower/watermelon/infra/resolver/etcd"
)

func TestAsyncFinder(t *testing.T) {
	watermelon.RegisterEtcdClient(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	})

	org := "spacegrower-test"
	ns := "space"
	serviceName := "space.Meta"

	finder := etcd.MustSetupEtcdAsyncFinder(etcd.NewEtcdTarget(org, ns, serviceName),
		func(query url.Values, attr etcdregister.NodeMeta, addr *resolver.Address) bool {
			return true
		})

	finder.ResolveNow(resolver.ResolveNowOptions{})

	addrs := finder.GetCurrentResults()

	for _, v := range addrs {
		nodeMeta, ok := etcd.GetBalancerAttributes[etcdregister.NodeMeta](v)
		if !ok {
			t.Fatal("failed to get nodeMeta")
		}

		t.Log("node", v.Addr)
		t.Log("node weight", nodeMeta.Weight)
	}
}
