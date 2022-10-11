package etcd

import (
	"context"
	"encoding/json"
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
	resolver.Register(new(etcdResolver))
}

const (
	ETCDResolverScheme = "watermelonetcdv3"
)

func MustSetupEtcdResolver() *etcdResolver {
	return &etcdResolver{
		client: manager.MustResolveEtcdClient(),
	}
}

type etcdResolver struct {
	client    *clientv3.Client
	cc        resolver.ClientConn
	namespace string
	prefixKey string
	log       wlog.Logger

	serviceConfig *wresolver.CustomizeServiceConfig
}

func (r *etcdResolver) Scheme() string {
	return ETCDResolverScheme
}

func (r *etcdResolver) GenerateTarget(serviceNameWithNs string) string {
	return fmt.Sprintf("%s://%s/%s", ETCDResolverScheme, "", serviceNameWithNs)
}

func (r *etcdResolver) buildResolveKey(service string) string {
	return filepath.ToSlash(filepath.Join(etcd.ETCD_KEY_PREFIX, service))
}

func (r *etcdResolver) ResolveNow(_ resolver.ResolveNowOptions) {}
func (r *etcdResolver) Close() {
	r.client.Close()
}

func (r *etcdResolver) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (
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
		} else if addr, err := parseNodeInfo(v.Key, v.Value); err != nil {
			r.log.Error("failed to parse node")
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

func parseNodeInfo(key, val []byte) (resolver.Address, error) {
	addr := resolver.Address{Addr: filepath.ToSlash(filepath.Base(string(key)))}
	var attr register.NodeMeta
	if err := json.Unmarshal(val, &attr); err != nil {
		return addr, err
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
