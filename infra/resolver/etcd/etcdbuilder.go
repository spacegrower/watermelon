package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"

	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

const (
	ETCDResolverScheme = "watermelonetcdv3"
)

func MustSetupEtcdResolver(region string) wresolver.Resolver {
	return NewEtcdResolver(manager.MustResolveEtcdClient(), region)
}

func NewEtcdResolver(client *clientv3.Client, region string) wresolver.Resolver {
	ks := &kvstore{
		client: client,
		region: region,
		log:    wlog.With(zap.String("component", "etcd-resolver")),
	}

	resolver.Register(ks)
	return ks
}

type kvstore struct {
	ctx       context.Context
	cancel    context.CancelFunc
	client    *clientv3.Client
	namespace string
	prefixKey string
	log       wlog.Logger

	region string
}

func (r *kvstore) Scheme() string {
	return ETCDResolverScheme
}

func (r *kvstore) GenerateTarget(fullServiceName string) string {
	return fmt.Sprintf("%s://%s/%s", ETCDResolverScheme, "", fullServiceName)
}

func (r *kvstore) buildResolveKey(service string) string {
	return filepath.ToSlash(filepath.Join(etcd.GetETCDPrefixKey(), service)) + "/"
}

func (r *kvstore) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (
	resolver.Resolver, error) {

	if r.client == nil {
		r.client = manager.MustResolveEtcdClient()
	}
	service := filepath.ToSlash(filepath.Base(target.URL.Path))

	ctx, cancel := context.WithCancel(context.Background())
	rr := &etcdResolver{
		ctx:       ctx,
		cancel:    cancel,
		client:    r.client,
		prefixKey: r.buildResolveKey(target.URL.Path),
		service:   service,
		region:    r.region,
		target:    target,
		opts:      opts,
		log:       wlog.With(zap.String("component", "etcd-resolver")),
	}

	update := func() {
		var (
			addrs []resolver.Address
			err   error
		)

		addrs, err = rr.resolve()
		if err != nil {
			r.log.Error("failed to resolve service addresses", zap.Error(err), zap.String("service", service))
			addrs = []resolver.Address{
				wresolver.NilAddress,
			}
		}

		if err := cc.UpdateState(resolver.State{
			Addresses:     addrs,
			ServiceConfig: cc.ParseServiceConfig(wresolver.ParseCustomizeToGrpcServiceConfig(rr.serviceConfig)),
		}); err != nil {
			r.log.Error("failed to update connect state", zap.Error(err))
		}
	}

	go safe.Run(func() {
		rr.watch(update)
	})
	update()

	return rr, nil
}

type etcdResolver struct {
	ctx    context.Context
	cancel context.CancelFunc

	client        *clientv3.Client
	prefixKey     string
	service       string
	region        string
	target        resolver.Target
	cc            resolver.ClientConn
	opts          resolver.BuildOptions
	log           wlog.Logger
	serviceConfig *wresolver.CustomizeServiceConfig
}

func (r *etcdResolver) ResolveNow(_ resolver.ResolveNowOptions) {}
func (r *etcdResolver) Close() {
	r.cancel()
}

func (r *etcdResolver) resolve() ([]resolver.Address, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	resp, err := r.client.Get(ctx, r.prefixKey, clientv3.WithPrefix())
	if err != nil {
		r.log.Error("failed to resolve service nodes", zap.Error(err))
		return nil, err
	}

	var result []resolver.Address
	for _, v := range resp.Kvs {
		if latest := filepath.Base(string(v.Key)); latest == "config" {
			if r.serviceConfig, err = parseServiceConfig(v.Value); err != nil {
				return nil, err
			}
		} else if addr, err := parseNodeInfo(v.Key, v.Value, func(attr register.NodeMeta, addr *resolver.Address) bool {
			if r.region == "" {
				return true
			}

			if attr.Region != r.region {
				proxy := manager.ResolveProxy(attr.Region)
				if proxy == "" {
					return false
				}

				addr.Addr = proxy
			}
			return true

		}); err != nil {
			if err != filterError {
				r.log.Error("parse node info with error", zap.Error(err))
			}
		} else {
			result = append(result, addr)
		}
	}

	if len(result) == 0 {
		return []resolver.Address{wresolver.NilAddress}, nil
	}

	if r.serviceConfig == nil {
		r.serviceConfig, _ = parseServiceConfig([]byte(wresolver.GetDefaultGrpcServiceConfig()))
	}

	if r.serviceConfig.Disabled {
		return []resolver.Address{wresolver.Disabled}, nil
	}

	return result, nil
}

func parseServiceConfig(val []byte) (*wresolver.CustomizeServiceConfig, error) {
	result := wresolver.CustomizeServiceConfig{}
	if err := json.Unmarshal(val, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

var filterError = errors.New("filter")

func parseNodeInfo(key, val []byte, allowFunc func(attr register.NodeMeta, addr *resolver.Address) bool) (resolver.Address, error) {
	addr := resolver.Address{Addr: filepath.ToSlash(filepath.Base(string(key)))}
	var attr register.NodeMeta
	if err := json.Unmarshal(val, &attr); err != nil {
		return addr, err
	}

	if ok := allowFunc(attr, &addr); !ok {
		return addr, filterError
	}

	addr.BalancerAttributes = attributes.New(register.NodeMetaKey{}, attr)

	return addr, nil
}

func (r *etcdResolver) watch(update func()) {
	var opts []clientv3.OpOption
	opts = append(opts, clientv3.WithPrefix())

	for {
		r.log.Debug("watch prefix " + r.prefixKey)
		updates := r.client.Watch(context.Background(), r.prefixKey, opts...)
		for {
			ev, ok := <-updates
			if !ok {
				// watcher closed
				r.log.Info("watch chan closed", zap.String("watch_key", r.prefixKey))
				return
			}

			r.log.Debug("resolved event",
				zap.String("watch_key", r.prefixKey),
				zap.Bool("canceled", ev.Canceled),
				zap.Error(ev.Err()))

			update()

			// some error occurred, re watch
			if ev.Err() != nil {
				r.log.Error("watch with error",
					zap.Error(ev.Err()),
					zap.Bool("canceled", ev.Canceled))

				break
			}
		}
	}
}
