package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"

	"github.com/spacegrower/watermelon/etc/remote/spacegrower/remote"
	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	wresolver "github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

func init() {
	// registrar resolver builder
	resolver.Register(new(remoteRegistry))
}

const (
	RemoteResolverScheme = "watermeloneremote"
)

func NewRemoteResolver(endpoint, region string, opts ...grpc.DialOption) (wresolver.Resolver, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cc, err := grpc.DialContext(ctx, endpoint, opts...)
	if err != nil {
		cancel()
		return nil, err
	}

	rr := &remoteRegistry{
		ctx:    ctx,
		cancel: cancel,
		region: region,
		log:    wlog.With(zap.String("component", "remote-resolver-builder")),
		client: remote.NewRegistryClient(cc),
	}

	return rr, nil
}

type remoteRegistry struct {
	ctx         context.Context
	cancel      context.CancelFunc
	client      remote.RegistryClient
	grpcOptions []grpc.DialOption
	namespace   string
	log         wlog.Logger

	region string
}

func (r *remoteRegistry) Scheme() string {
	return RemoteResolverScheme
}

func (r *remoteRegistry) GenerateTarget(serviceNameWithNs string) string {
	return fmt.Sprintf("%s://%s/%s", RemoteResolverScheme, "", serviceNameWithNs)
}

func (r *remoteRegistry) Build(target resolver.Target, cc resolver.ClientConn, opts resolver.BuildOptions) (
	resolver.Resolver, error) {

	service := filepath.ToSlash(filepath.Base(target.URL.Path))

	remoteResovler, err := r.client.Resolver(r.ctx,
		&remote.TargetInfo{
			Service: target.URL.Path,
		})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(r.ctx)
	rr := &remoteResolver{
		ctx:    ctx,
		cancel: cancel,

		client:  remoteResovler,
		service: service,
		region:  r.region,
		target:  target,
		cc:      cc,
		opts:    opts,
		log:     wlog.With(zap.String("component", "remote-resolver")),
	}

	update := func(resp *remote.ResolveInfo) {
		var addrs []resolver.Address
		config, err := parseServiceConfig(resp.Config)
		if err != nil {
			r.log.Error("failed to parse service config", zap.Error(err), zap.String("service", service))
			addrs = []resolver.Address{
				wresolver.NilAddress,
			}
		} else {
			rr.log.Debug("new receive config", zap.String("config", string(resp.Config)))

			if config.Disabled {
				addrs = []resolver.Address{
					wresolver.Disabled,
				}
			} else {
				addrs, err := rr.resolve(resp)
				if err != nil {
					r.log.Error("failed to resolve service addresses", zap.Error(err), zap.String("service", service))
					addrs = []resolver.Address{
						wresolver.NilAddress,
					}
				} else {
					rr.log.Debug("new receive address", zap.Any("address", addrs))
				}
			}
		}

		if err := cc.UpdateState(resolver.State{
			Addresses:     addrs,
			ServiceConfig: cc.ParseServiceConfig(wresolver.ParseCustomizeToGrpcServiceConfig(config)),
		}); err != nil {
			r.log.Error("failed to update connect state", zap.Error(err))
		}
	}

	go safe.Run(func() {
		watch(rr, update)
	})

	return rr, nil
}

type remoteResolver struct {
	ctx    context.Context
	cancel context.CancelFunc

	client remote.Registry_ResolverClient

	service string
	region  string
	target  resolver.Target
	cc      resolver.ClientConn
	opts    resolver.BuildOptions
	run     func() (revc func() (*remote.ResolveInfo, error), update func(*remote.ResolveInfo))
	log     wlog.Logger
}

func (r *remoteResolver) ResolveNow(_ resolver.ResolveNowOptions) {}
func (r *remoteResolver) Close() {
	r.cancel()
}

func (r *remoteResolver) resolve(resp *remote.ResolveInfo) ([]resolver.Address, error) {
	var result []resolver.Address
	for addr, conf := range resp.Address {
		addr, err := parseNodeInfo(addr, conf, func(attr register.NodeMeta, addr *resolver.Address) bool {
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

		})
		if err != nil {
			if err != filterError {
				r.log.Error("parse node info with error", zap.Error(err))
			}
			continue
		}
		result = append(result, addr)
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

func parseNodeInfo(key string, val []byte, allowFunc func(attr register.NodeMeta, addr *resolver.Address) bool) (resolver.Address, error) {
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

func watch(rr *remoteResolver, update func(*remote.ResolveInfo)) {

	for {
		select {
		case <-rr.ctx.Done():
			return
		default:
		}

		recv, err := rr.client.Recv()
		if err != nil {
			update(&remote.ResolveInfo{})
			rr.Close()
			return
		}

		update(recv)
	}

}
