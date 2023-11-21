package infra

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/resolver/etcd"
)

type ClientServiceNameGenerator interface {
	FullServiceName(srvName string) string
	ProxyMetadata() metadata.MD
}

type Client[T ClientServiceNameGenerator] struct {
	CustomizeMeta T
}

type COptions[T ClientServiceNameGenerator] struct {
	CustomizeMeta T
	dialOptions   []grpc.DialOption
	resolver      resolver.Resolver
	timeout       time.Duration
}

type ClientOptions[T ClientServiceNameGenerator] func(c *COptions[T])

func WithServiceResolver[T ClientServiceNameGenerator](r resolver.Resolver) ClientOptions[T] {
	return func(c *COptions[T]) {
		c.resolver = r
	}
}

func WithDialTimeout[T ClientServiceNameGenerator](t time.Duration) ClientOptions[T] {
	return func(c *COptions[T]) {
		c.timeout = t
	}
}

func WithGrpcDialOptions[T ClientServiceNameGenerator](opts ...grpc.DialOption) ClientOptions[T] {
	return func(c *COptions[T]) {
		c.dialOptions = opts
	}
}

func injectMetadata(ctx context.Context, md metadata.MD) context.Context {
	if len(md) == 0 {
		return ctx
	}
	proxyMetadata := md
	var pairs []string
	for k, vs := range proxyMetadata {
		for _, v := range vs {
			if v != "" {
				pairs = append(pairs, k, v)
			}
		}
	}
	if len(pairs) > 0 {
		ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
	}
	return ctx
}

func NewClientConn[T ClientServiceNameGenerator](serviceName string, opts ...ClientOptions[T]) (*grpc.ClientConn, error) {
	options := &COptions[T]{
		timeout: time.Second * 5,
	}

	for _, opt := range opts {
		opt(options)
	}

	options.dialOptions = append(options.dialOptions,
		grpc.WithDefaultServiceConfig(resolver.GetDefaultGrpcServiceConfig()),
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoker(injectMetadata(ctx, options.CustomizeMeta.ProxyMetadata().Copy()), method, req, reply, cc, opts...)
		}),
		grpc.WithChainStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return streamer(injectMetadata(ctx, options.CustomizeMeta.ProxyMetadata().Copy()), desc, cc, method, opts...)
		}),
	)

	if options.resolver == nil {
		options.resolver = etcd.MustSetupEtcdResolver()
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx,
		options.resolver.GenerateTarget(options.CustomizeMeta.FullServiceName(serviceName)),
		options.dialOptions...)
	if err != nil {
		return nil, err
	}

	return cc, nil
}
