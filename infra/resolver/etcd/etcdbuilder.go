package etcd

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/resolver"

	wb "github.com/spacegrower/watermelon/infra/balancer"
	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

type gRPCAttributeComparable interface{ Equal(any) bool }

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

func (r ResolveMeta) ProxyMetadata() metadata.MD {
	md := metadata.New(map[string]string{})
	if r.OrgID != "" {
		md.Set("watermelon-org-id", r.OrgID)
	}
	if r.Region != "" {
		md.Set("watermelon-region", r.Region)
	}
	if r.Namespace != "" {
		md.Set("watermelon-namespace", r.Namespace)
	}
	return md
}

func init() {
	balancer.Register(base.NewBalancerBuilder(wb.WeightRobinName,
		new(wb.WeightRobinBalancer[etcd.NodeMeta]),
		base.Config{HealthCheck: true}))
}

var (
	ETCDResolverScheme = "watermelonetcdv3"
)

type AllowFuncType[T gRPCAttributeComparable] func(query url.Values, attr T, addr *resolver.Address) bool

func DefaultAllowFunc(query url.Values, attr etcd.NodeMeta, addr *resolver.Address) bool {
	region := query.Get("region")
	if region == "" {
		return true
	}

	if attr.Region != region {
		if attr.Tags != nil {
			if endpoint := register.GetEndpointFromTags(attr.Tags); endpoint != "" {
				addr.Addr = endpoint
				addr.ServerName = endpoint
				return true
			}
		}
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

var registered = sync.Once{}

func NewEtcdResolver[T gRPCAttributeComparable](client *clientv3.Client, af AllowFuncType[T]) wresolver.Resolver {
	ks := &kvstore[T]{
		client:    client,
		log:       wlog.With(zap.String("component", "etcd-resolver")),
		allowFunc: af,
	}

	registered.Do(func() {
		resolver.Register(ks)
	})
	return ks
}

type kvstore[T gRPCAttributeComparable] struct {
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

func buildResolveKey(service string) string {
	return filepath.ToSlash(filepath.Join(etcd.GetETCDPrefixKey(), service)) + "/"
}

func (r *kvstore[T]) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (
	resolver.Resolver, error) {

	if r.client == nil {
		r.client = manager.MustResolveEtcdClient()
	}

	ctx, cancel := context.WithCancel(context.Background())
	etcdResolver := &etcdResolver[T]{
		ctx:       ctx,
		cancel:    cancel,
		client:    r.client,
		prefixKey: buildResolveKey(target.URL.Path),
		service:   filepath.Base(filepath.ToSlash(target.URL.Path)),
		target:    target,
		log:       wlog.With(zap.String("component", "etcd-resolver")),
		allowFunc: r.allowFunc,
	}

	var (
		currentConfig    string
		currentAddrsHash string
		resolveLocker    sync.Mutex
	)

	etcdResolver.resolveNow = func() {
		resolveLocker.Lock()
		defer resolveLocker.Unlock()

		addrs, err := etcdResolver.resolve()
		if err != nil {
			r.log.Error("failed to resolve service addresses", zap.Error(err), zap.String("service", etcdResolver.service))
			return
		}

		sort.Slice(addrs, func(i, j int) bool {
			return addrs[i].Addr > addrs[j].Addr
		})

		cfg := wresolver.ParseCustomizeToGrpcServiceConfig(etcdResolver.serviceConfig)
		addrsHash := addressHash(addrs)
		if cfg != currentConfig || addrsHash != currentAddrsHash {
			if err := cc.UpdateState(resolver.State{
				Addresses:     addrs,
				ServiceConfig: cc.ParseServiceConfig(cfg),
			}); err != nil {
				r.log.Error("failed to update connect state", zap.Error(err))
				return
			}
			currentConfig = cfg
			currentAddrsHash = addrsHash
		}
	}

	go safe.Run(func() {
		etcdResolver.startResolve()
	})
	etcdResolver.resolveNow()

	return etcdResolver, nil
}

func addressHash(addrs []resolver.Address) string {
	s := strings.Builder{}
	for _, v := range addrs {
		s.WriteString(v.String())
	}

	h := md5.New()
	h.Write([]byte(s.String()))
	cipherStr := h.Sum(nil)
	return hex.EncodeToString(cipherStr)
}

type etcdResolver[T gRPCAttributeComparable] struct {
	ctx    context.Context
	cancel context.CancelFunc

	client        *clientv3.Client
	prefixKey     string
	service       string
	target        resolver.Target
	log           wlog.Logger
	resolveNow    func()
	serviceConfig *wresolver.CustomizeServiceConfig
	currentResult []resolver.Address

	allowFunc AllowFuncType[T]
}

func (r *etcdResolver[T]) GetCurrentResults() []resolver.Address {
	return r.currentResult
}

func (r *etcdResolver[T]) ResolveNow(_ resolver.ResolveNowOptions) {
	if r.ctx.Err() != nil {
		r.ctx, r.cancel = context.WithCancel(context.Background())
		go safe.Run(func() {
			r.startResolve()
		})
	}
	r.resolveNow()
}
func (r *etcdResolver[T]) Close() {
	r.cancel()
	r.log.Debug("closed")
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

	if r.serviceConfig == nil {
		r.serviceConfig, _ = parseServiceConfig([]byte(wresolver.GetDefaultGrpcServiceConfig()))
	}

	if r.serviceConfig.Disabled {
		return []resolver.Address{wresolver.Disabled}, nil
	}

	r.currentResult = result
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

	if addr.Attributes == nil {
		addr.Attributes = attributes.New(register.NodeMetaKey{}, attr)
	} else {
		addr.Attributes.WithValue(register.NodeMetaKey{}, attr)
	}

	return addr, nil
}

func (r *etcdResolver[T]) startResolve() {
	var opts []clientv3.OpOption
	opts = append(opts, clientv3.WithPrefix())
	defer r.cancel()

	watchLogic := func() error {
		watcher := clientv3.NewWatcher(r.client)
		defer watcher.Close()

		updates := watcher.Watch(r.ctx, r.prefixKey, opts...)
		for {
			ev, ok := <-updates
			if !ok {
				// watcher closed
				r.log.Info("watch chan closed", zap.String("watch_key", r.prefixKey))
				return errors.New("watch chan closed")
			}

			r.log.Debug("resolved event",
				zap.String("watch_key", r.prefixKey),
				zap.Bool("canceled", ev.Canceled),
				zap.Error(ev.Err()))

			r.resolveNow()

			// some error occurred, re watch
			if ev.Err() != nil {
				r.log.Error("watch with error",
					zap.Error(ev.Err()),
					zap.Bool("canceled", ev.Canceled))
				time.Sleep(time.Second)
				return nil
			}
		}
	}

	for {
		if err := watchLogic(); err != nil {
			return
		}
	}
}

func GetMetaAttributes[T any](addr resolver.Address) (T, bool) {
	var nodeMeta T
	attr := addr.Attributes.Value(register.NodeMetaKey{})
	if attr == nil {
		return nodeMeta, false

	}
	nodeMeta, ok := attr.(T)
	return nodeMeta, ok
}
