package infra

import (
	"context"
	"path/filepath"
	"time"

	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/resolver/etcd"
)

type client struct {
}

type clientOptions struct {
	namespace   string
	dialOptions []grpc.DialOption
	resolver    resolver.Resolver
	timeout     time.Duration
	region      string
}

type ClientOptions func(c *clientOptions)

func (*ClientConn) WithServiceResolver(r resolver.Resolver) ClientOptions {
	return func(c *clientOptions) {
		c.resolver = r
	}
}

func (*ClientConn) WithNamespace(ns string) ClientOptions {
	return func(c *clientOptions) {
		c.namespace = ns
	}
}

func (*ClientConn) WithDialTimeout(t time.Duration) ClientOptions {
	return func(c *clientOptions) {
		c.timeout = t
	}
}

func (*ClientConn) WithGrpcOptions(opts ...grpc.DialOption) ClientOptions {
	return func(c *clientOptions) {
		c.dialOptions = opts
	}
}

func (*ClientConn) WithRegion(region string) ClientOptions {
	return func(c *clientOptions) {
		c.region = region
	}
}

func newClientConn(serviceName string, opts ...ClientOptions) (*grpc.ClientConn, error) {
	options := &clientOptions{
		namespace: "default",
		timeout:   time.Second * 5,
	}

	for _, opt := range opts {
		opt(options)
	}

	options.dialOptions = append(options.dialOptions,
		grpc.WithDefaultServiceConfig(resolver.GetDefaultGrpcServiceConfig()),
	)

	if options.resolver == nil {
		options.resolver = etcd.MustSetupEtcdResolver(options.region)
	}

	ctx, cancel := context.WithTimeout(context.Background(), options.timeout)
	defer cancel()
	cc, err := grpc.DialContext(ctx,
		options.resolver.GenerateTarget(filepath.ToSlash(filepath.Join(options.namespace, serviceName))),
		options.dialOptions...)
	if err != nil {
		return nil, err
	}

	return cc, nil
}
