package infra

import (
	"context"
	"path/filepath"
	"strconv"
	"time"

	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/resolver"
	"github.com/spacegrower/watermelon/infra/resolver/etcd"
)

type client struct {
}

type clientOptions struct {
	orgid       int64
	namespace   string
	dialOptions []grpc.DialOption
	context     context.Context
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

func (*ClientConn) WithOrg(id int64) ClientOptions {
	return func(c *clientOptions) {
		c.orgid = id
	}
}

func (*ClientConn) WithNamespace(ns string) ClientOptions {
	return func(c *clientOptions) {
		c.namespace = ns
	}
}

func (*ClientConn) WithDialTimeout(t time.Duration) ClientOptions {
	return func(c *clientOptions) {
		c.context, _ = context.WithTimeout(context.Background(), t)
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

func newClientConn(serviceName string, opts ...ClientOptions) (grpc.ClientConnInterface, error) {
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
		options.resolver = etcd.MustSetupEtcdResolver(options.region)
	}

	cc, err := grpc.DialContext(options.context,
		options.resolver.GenerateTarget(filepath.ToSlash(filepath.Join(strconv.FormatInt(options.orgid, 10), options.namespace, serviceName))),
		options.dialOptions...)
	if err != nil {
		return nil, err
	}

	return cc, nil
}
