package etcd

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/resolver"

	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)



type Finder[T gRPCAttributeComparable] struct {
	client *clientv3.Client
	logger wlog.Logger
}

func NewFinder[T gRPCAttributeComparable](client *clientv3.Client) *Finder[T] {
	return &Finder[T]{
		client: client,
		logger: wlog.With(zap.String("component", "finder")),
	}
}

func NewEtcdTarget(org, ns, service string) resolver.Target {
	return resolver.Target{
		URL: url.URL{
			Scheme: ETCDResolverScheme,
			Host:   "",
			Path:   fmt.Sprintf("/%s/%s/%s", org, ns, service),
		},
	}
}

type AsyncFinder interface {
	GetCurrentResults() []resolver.Address
	ResolveNow(_ resolver.ResolveNowOptions)
	Close()
}

func NewAsyncFinder[T gRPCAttributeComparable](client *clientv3.Client, target resolver.Target, allowFunc AllowFuncType[T]) AsyncFinder {
	ctx, cancel := context.WithCancel(context.Background())

	rr := &etcdResolver[T]{
		ctx:       ctx,
		cancel:    cancel,
		client:    client,
		prefixKey: buildResolveKey(target.URL.Path),
		service:   filepath.Base(filepath.ToSlash(target.URL.Path)),
		target:    target,
		log:       wlog.With(zap.String("component", "etcd-async-finder")),
		allowFunc: allowFunc,
	}

	rr.resolveNow = func() {
		addrs, err := rr.resolve()
		if err != nil {
			rr.log.Error("failed to resolve service addresses", zap.Error(err), zap.String("service", rr.service))
		}

		rr.currentResult = addrs
	}

	go safe.Run(func() {
		rr.startResolve()
	})
	rr.resolveNow()
	return rr
}

func (f *Finder[T]) FindAll(ctx context.Context, prefix string) (address []resolver.Address, config *wresolver.CustomizeServiceConfig, err error) {
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

			address = append(address, addr)
		}
	}
	return
}

func MustSetupEtcdFinder() *Finder[etcd.NodeMeta] {
	return NewFinder[etcd.NodeMeta](manager.MustResolveEtcdClient())
}

func MustSetupEtcdAsyncFinder(target resolver.Target, allowFunc AllowFuncType[etcd.NodeMeta]) AsyncFinder {
	return NewAsyncFinder[etcd.NodeMeta](manager.MustResolveEtcdClient(), target, allowFunc)
}
