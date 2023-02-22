package infra

import (
	"context"
	"time"

	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/resolver/etcd"
)

type ClientServiceNameGenerator interface {
	FullServiceName(srvName string) string
}

type Client[T ClientServiceNameGenerator] struct {
	CustomeMeta T
}

type COptions[T ClientServiceNameGenerator] struct {
	CustomeMeta T
	dialOptions []grpc.DialOption
	resolver    resolver.Resolver
	timeout     time.Duration
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

func NewClientConn[T ClientServiceNameGenerator](serviceName string, opts ...ClientOptions[T]) (*grpc.ClientConn, error) {
	options := &COptions[T]{
		timeout: time.Second * 5,
	}

	for _, opt := range opts {
		opt(options)
	}

	options.dialOptions = append(options.dialOptions,
		grpc.WithDefaultServiceConfig(resolver.GetDefaultGrpcServiceConfig()),
	)

	if options.resolver == nil {
		options.resolver = etcd.MustSetupEtcdResolver()
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx,
		options.resolver.GenerateTarget(options.CustomeMeta.FullServiceName(serviceName)),
		options.dialOptions...)
	if err != nil {
		return nil, err
	}

	return cc, nil
}
