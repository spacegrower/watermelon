package infra

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/resolver/etcd"
)

type ClientConn struct {
	*grpc.ClientConn
	md metadata.MD
}

func (c *ClientConn) JoinMetadata(ctx context.Context) context.Context {
	if len(c.md) > 0 {
		var pairs []string
		for k, vs := range c.md {
			for _, v := range vs {
				if v != "" {
					pairs = append(pairs, k, v)
				}
			}
		}
		if len(pairs) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
		}
	}
	return ctx
}

func (cc *ClientConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return cc.ClientConn.NewStream(cc.JoinMetadata(ctx), desc, method, opts...)
}

func (cc *ClientConn) Invoke(ctx context.Context, method string, args, reply interface{}, opts ...grpc.CallOption) error {
	return cc.ClientConn.Invoke(cc.JoinMetadata(ctx), method, args, reply, opts...)
}

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

func NewClientConn[T ClientServiceNameGenerator](serviceName string, opts ...ClientOptions[T]) (*ClientConn, error) {
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
		options.resolver.GenerateTarget(options.CustomizeMeta.FullServiceName(serviceName)),
		options.dialOptions...)
	if err != nil {
		return nil, err
	}

	return &ClientConn{
		ClientConn: cc,
		md:         options.CustomizeMeta.ProxyMetadata().Copy(),
	}, nil
}
