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
	context     context.Context
	resolver    resolver.Resolver
	timeout     time.Duration
	region      string
}

type ClientOptions func(c *clientOptions)

func ClientWithServiceResolver(r resolver.Resolver) ClientOptions {
	return func(c *clientOptions) {
		c.resolver = r
	}
}

func ClientWithNamespace(ns string) ClientOptions {
	return func(c *clientOptions) {
		c.namespace = ns
	}
}

func ClientWithDialTimeout(t time.Duration) ClientOptions {
	return func(c *clientOptions) {
		c.context, _ = context.WithTimeout(context.Background(), t)
	}
}

func ClientWithGrpcOptions(opts ...grpc.DialOption) ClientOptions {
	return func(c *clientOptions) {
		c.dialOptions = opts
	}
}

func ClientWithRegion(region string) ClientOptions {
	return func(c *clientOptions) {
		c.region = region
	}
}

func NewClientConn(serviceName string, opts ...ClientOptions) (grpc.ClientConnInterface, error) {
	options := &clientOptions{
		namespace: "default",
		context:   context.Background(),
	}

	for _, opt := range opts {
		opt(options)
	}

	options.dialOptions = append(options.dialOptions,
		grpc.WithDefaultServiceConfig(resolver.GetDefaultGrpcServiceConfig()),
	)

	if options.resolver == nil {
		etcd.MustSetupEtcdResolver(options.region)
	}

	cc, err := grpc.DialContext(options.context,
		options.resolver.GenerateTarget(filepath.ToSlash(filepath.Join(options.namespace, serviceName))),
		options.dialOptions...)
	if err != nil {
		return nil, err
	}

	return cc, nil
}
