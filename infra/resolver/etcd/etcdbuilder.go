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

func init() {
	// registrar resolver builder
	resolver.Register(new(kvstore))
}

const (
	ETCDResolverScheme = "watermelonetcdv3"
)

func MustSetupEtcdResolver(region string) wresolver.Resolver {
	return &kvstore{
		client: manager.MustResolveEtcdClient(),
		region: region,
	}
}

func NewEtcdResolver(client *clientv3.Client) wresolver.Resolver {
	return &kvstore{
		client: client,
		log:    wlog.With(zap.String("component", "etcd-register")),
	}
}

type kvstore struct {
	client    *clientv3.Client
	cc        resolver.ClientConn
	namespace string
	prefixKey string
	log       wlog.Logger

	region string

	serviceConfig *wresolver.CustomizeServiceConfig
}

func (r *kvstore) Scheme() string {
	return ETCDResolverScheme
}

func (r *kvstore) GenerateTarget(serviceNameWithNs string) string {
	return fmt.Sprintf("%s://%s/%s", ETCDResolverScheme, "", serviceNameWithNs)
}

func (r *kvstore) buildResolveKey(service string) string {
	return filepath.ToSlash(filepath.Join(etcd.ETCD_KEY_PREFIX, service))
}

func (r *kvstore) ResolveNow(_ resolver.ResolveNowOptions) {}
func (r *kvstore) Close() {
	r.client.Close()
}

func (r *kvstore) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (
	resolver.Resolver, error) {

	if r.client == nil {
		r.client = manager.MustResolveEtcdClient()
	}
	r.log = wlog.With(zap.String("component", "etcd-resolver"))
	service := filepath.ToSlash(filepath.Base(target.URL.Path))
	r.prefixKey = r.buildResolveKey(target.URL.Path)

	update := func() {
		var addrs []resolver.Address
		addrs, err := r.resolve()
		if err != nil {
			r.log.Error("failed to resolve service addresses", zap.Error(err), zap.String("service", service))
			addrs = []resolver.Address{
				wresolver.NilAddress,
			}
		}
		if err := cc.UpdateState(resolver.State{
			Addresses:     addrs,
			ServiceConfig: cc.ParseServiceConfig(wresolver.ParseCustomizeToGrpcServiceConfig(r.serviceConfig)),
		}); err != nil {
			r.log.Error("failed to update connect state", zap.Error(err))
		}
	}

	go safe.Run(func() {
		r.watch(update)
	})
	update()

	return r, nil
}

func (r *kvstore) resolve() ([]resolver.Address, error) {
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

func (r *kvstore) watch(update func()) {
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
