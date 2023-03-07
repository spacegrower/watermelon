package etcd

import (
	"context"
	"path/filepath"

	"github.com/spacegrower/watermelon/infra/internal/manager"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/resolver"
)

type Finder struct {
	client *clientv3.Client
	logger wlog.Logger
}

func NewFinder(client *clientv3.Client) *Finder {
	return &Finder{
		client: client,
		logger: wlog.With(zap.String("component", "finder")),
	}
}

func (f *Finder) FindAll(ctx context.Context, prefix string) (address []string, config *wresolver.CustomizeServiceConfig, err error) {
	resp, err := f.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		f.logger.Error("failed to resolve service nodes", zap.Error(err))
		return
	}

	for _, v := range resp.Kvs {
		if latest := filepath.Base(string(v.Key)); latest == "config" {
			if config, err = parseServiceConfig(v.Value); err != nil {
				return
			}
		} else {
			addr, err := parseNodeInfo(v.Key, v.Value, func(attr any, addr *resolver.Address) bool {
				return true
			})
			if err != nil {
				return nil, nil, err
			}
			address = append(address, addr.Addr)
		}
	}
	return
}

func MustSetupEtcdFinder() *Finder {
	return NewFinder(manager.MustResolveEtcdClient())
}
