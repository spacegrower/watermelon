package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	wb "github.com/spacegrower/watermelon/infra/balancer"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/resolver"

	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

func DefaultResolveMeta() ResolveMeta {
	return ResolveMeta{
		OrgID:     "default",
		Region:    "default",
		Namespace: "default",
	}
}

type ResolveMeta struct {
	OrgID     string
	Region    string
	Namespace string
}

func (r ResolveMeta) FullServiceName(srvName string) string {
	target := filepath.ToSlash(filepath.Join(r.OrgID, r.Namespace, srvName))
	path, _ := url.Parse(target)
	if r.Region != "" {
		query := path.Query()
		query.Add("region", r.Region)
		path.RawQuery = query.Encode()
	}
	return path.String()
}

func init() {
	balancer.Register(base.NewBalancerBuilder(wb.WeightRobinName,
		new(wb.WeightRobinBalancer[etcd.NodeMeta]),
		base.Config{HealthCheck: true}))
}

var (
	ETCDResolverScheme = "watermelonetcdv3"
)

type AllowFuncType[T any] func(query url.Values, attr T, addr *resolver.Address) bool

func DefaultAllowFunc(query url.Values, attr etcd.NodeMeta, addr *resolver.Address) bool {
	region := query.Get("region")
	if region == "" {
		return true
	}

	if attr.Region != region {
		proxy := manager.ResolveProxy(attr.Region)
		if proxy == "" {
			return false
		}

		addr.Addr = proxy
		addr.ServerName = proxy
	}
	return true
}

func MustSetupEtcdResolver() wresolver.Resolver {
	return NewEtcdResolver(manager.MustResolveEtcdClient(), DefaultAllowFunc)
}

func NewEtcdResolver[T any](client *clientv3.Client, af AllowFuncType[T]) wresolver.Resolver {
	ks := &kvstore[T]{
		client:    client,
		log:       wlog.With(zap.String("component", "etcd-resolver")),
		allowFunc: af,
	}

	resolver.Register(ks)
	return ks
}

type kvstore[T any] struct {
	ctx       context.Context
	cancel    context.CancelFunc
	client    *clientv3.Client
	prefixKey string
	log       wlog.Logger

	allowFunc AllowFuncType[T]
}

func (r *kvstore[T]) Scheme() string {
	return ETCDResolverScheme
}

func (r *kvstore[T]) GenerateTarget(fullServiceName string) string {
	return fmt.Sprintf("%s://%s/%s", ETCDResolverScheme, "", fullServiceName)
}

func (r *kvstore[T]) buildResolveKey(service string) string {
	return filepath.ToSlash(filepath.Join(etcd.GetETCDPrefixKey(), service)) + "/"
}

func (r *kvstore[T]) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (
	resolver.Resolver, error) {

	if r.client == nil {
		r.client = manager.MustResolveEtcdClient()
	}

	service := filepath.Base(filepath.ToSlash(target.URL.Path))

	ctx, cancel := context.WithCancel(context.Background())
	rr := &etcdResolver[T]{
		ctx:       ctx,
		cancel:    cancel,
		client:    r.client,
		prefixKey: r.buildResolveKey(target.URL.Path),
		service:   service,
		target:    target,
		opts:      opts,
		log:       wlog.With(zap.String("component", "etcd-resolver")),
		allowFunc: r.allowFunc,
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

type etcdResolver[T any] struct {
	ctx    context.Context
	cancel context.CancelFunc

	client        *clientv3.Client
	prefixKey     string
	service       string
	target        resolver.Target
	cc            resolver.ClientConn
	opts          resolver.BuildOptions
	log           wlog.Logger
	serviceConfig *wresolver.CustomizeServiceConfig

	allowFunc AllowFuncType[T]
}

func (r *etcdResolver[T]) ResolveNow(_ resolver.ResolveNowOptions) {}
func (r *etcdResolver[T]) Close() {
	r.cancel()
}

func (r *etcdResolver[T]) resolve() ([]resolver.Address, error) {
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
		} else if addr, err := parseNodeInfo(v.Key, v.Value, func(attr T, addr *resolver.Address) bool {
			return r.allowFunc(r.target.URL.Query(), attr, addr)
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

func parseNodeInfo[T any](key, val []byte, allowFunc func(attr T, addr *resolver.Address) bool) (resolver.Address, error) {
	addr := resolver.Address{Addr: filepath.ToSlash(filepath.Base(string(key)))}
	var attr T
	if err := json.Unmarshal(val, &attr); err != nil {
		return addr, err
	}

	if ok := allowFunc(attr, &addr); !ok {
		return addr, filterError
	}

	addr.BalancerAttributes = attributes.New(register.NodeMetaKey{}, attr)

	return addr, nil
}

func (r *etcdResolver[T]) watch(update func()) {
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
