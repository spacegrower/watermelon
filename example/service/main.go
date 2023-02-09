package main

import (
	"context"
	"fmt"
	"syscall"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacegrower/watermelon/etc/example/book"
	"github.com/spacegrower/watermelon/etc/example/greeter"
	"github.com/spacegrower/watermelon/infra"
	"github.com/spacegrower/watermelon/infra/middleware"
	"github.com/spacegrower/watermelon/infra/register/etcd"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
)

type GreeterSrv struct {
	greeter.UnimplementedGreeterServer
}

func (g *GreeterSrv) SayHello(ctx context.Context, in *greeter.HelloRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{
		Message: "hello " + in.Name,
	}, nil
}

func (g *GreeterSrv) SayHelloAgain(ctx context.Context, in *greeter.HelloRequest) (*greeter.HelloReply, error) {
	return &greeter.HelloReply{
		Message: "hello " + in.Name,
	}, nil
}

type BookSrv struct {
	book.UnimplementedBookServer
}

func main() {
	// install logger
	wlog.SetGlobalLogger(wlog.NewLogger(&wlog.Config{
		Name:  "example/greeter",
		Level: wlog.DebugLevel,
	}))

	// register etcd client
	if err := infra.RegisterEtcdClient(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	}); err != nil {
		panic(err)
	}

	service := &GreeterSrv{}
	newServer := infra.NewServer()
	srv := newServer(func(srv *grpc.Server) {
		greeter.RegisterGreeterServer(srv, service)
		book.RegisterBookServer(srv, &BookSrv{})
	}, newServer.WithNamespace("test"),
		newServer.WithRegion("local"),
		newServer.WithServiceRegister(etcd.MustSetupEtcdRegister()))

	a := srv.Group()
	a.Use(func(ctx context.Context) error {
		if err := middleware.Next(ctx); err != nil {
			fmt.Println("return error", err)
			return err
		}
		fullMethod := middleware.GetFullMethodFrom(ctx)
		wlog.Info("finished: " + fullMethod)
		return nil
	})

	a.Handler(service.SayHello)

	b := a.Group()
	b.Use(func(ctx context.Context) error {
		fullMethod := middleware.GetFullMethodFrom(ctx)

		if utils.PathBase(fullMethod) == "SayHelloAgain" {
			return status.Error(codes.Aborted, "Don't say good things a second time")
		}
		return nil
	})
	b.Handler(service.SayHelloAgain)

	srv.RunUntil(syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
}
