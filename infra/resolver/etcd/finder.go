package etcd

import (
	"context"
	"path/filepath"

	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/resolver"
)

type Finder[T any] struct {
	client *clientv3.Client
	logger wlog.Logger
}

func NewFinder[T any](client *clientv3.Client) *Finder[T] {
	return &Finder[T]{
		client: client,
		logger: wlog.With(zap.String("component", "finder")),
	}
}

type FindedResult[T any] struct {
	addr string
	attr T
}

func (f *FindedResult[T]) Attr() T {
	return f.attr
}

func (f *FindedResult[T]) Address() string {
	return f.addr
}

func (f *Finder[T]) FindAll(ctx context.Context, prefix string) (address []FindedResult[T], config *wresolver.CustomizeServiceConfig, err error) {
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
			addr, err := parseNodeInfo(v.Key, v.Value, func(attr T, addr *resolver.Address) bool {
				return true
			})
			if err != nil {
				return nil, nil, err
			}

			address = append(address, FindedResult[T]{
				addr: addr.Addr,
				attr: addr.BalancerAttributes.Value(register.NodeMetaKey{}).(T),
			})
		}
	}
	return
}

func MustSetupEtcdFinder() *Finder[etcd.NodeMeta] {
	return NewFinder[etcd.NodeMeta](manager.MustResolveEtcdClient())
}
