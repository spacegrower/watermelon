package firemelon

import (
	"net/url"

	"github.com/spacegrower/watermelon/infra"
	"github.com/spacegrower/watermelon/infra/register"
	eregister "github.com/spacegrower/watermelon/infra/register/etcd"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	eresolve "github.com/spacegrower/watermelon/infra/resolver/etcd"
	"google.golang.org/grpc/resolver"
)

func MustSetupEtcdRegister() register.ServiceRegister[NodeMeta] {
	return eregister.NewEtcdRegister[NodeMeta](infra.ResolveEtcdClient())
}

func DefaultAllowFunc(query url.Values, attr NodeMeta, addr *resolver.Address) bool {
	region := query.Get("region")
	if region == "" {
		return true
	}

	if attr.Region != region {
		proxy := infra.ResolveProxy(attr.Region)
		if proxy == "" {
			return false
		}

		addr.Addr = proxy
		addr.ServerName = proxy
	}
	return true
}

func MustSetupEtcdResolver() wresolver.Resolver {
	return eresolve.NewEtcdResolver[NodeMeta](infra.ResolveEtcdClient(), DefaultAllowFunc)
}
