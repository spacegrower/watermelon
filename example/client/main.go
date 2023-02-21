package main

import (
	"context"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon"
	"github.com/spacegrower/watermelon/etc/example/book"
	"github.com/spacegrower/watermelon/etc/example/greeter"
	"github.com/spacegrower/watermelon/infra"
	"github.com/spacegrower/watermelon/infra/wlog"
)

func main() {
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

	newClientConn := watermelon.NewClientConn()
	cc, err := newClientConn(greeter.Greeter_ServiceDesc.ServiceName,
		newClientConn.WithNamespace("test"),
		newClientConn.WithRegion("local"),
		newClientConn.WithGrpcDialOptions(grpc.WithInsecure()))
	if err != nil {
		panic(err)
	}

	client := greeter.NewGreeterClient(cc)

	ctx, _ := context.WithTimeout(context.Background(), time.Second*3)
	resp, err := client.SayHello(ctx, &greeter.HelloRequest{
		Name: "HanMeiMei",
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(resp.Message)

	_, err = client.SayHelloAgain(ctx, &greeter.HelloRequest{
		Name: "HanMeiMei",
	})
	if err != nil {
		fmt.Println(err)
	}

	bookcc, err := newClientConn(book.Book_ServiceDesc.ServiceName,
		newClientConn.WithNamespace("test"),
		newClientConn.WithRegion("local"),
		newClientConn.WithGrpcDialOptions(grpc.WithInsecure()))
	if err != nil {
		panic(err)
	}

	bookclient := book.NewBookClient(bookcc)
	_, err = bookclient.GetBook(context.Background(), &book.GetBookRequest{Name: "testbook"})
	if err != nil {
		// will got unimplemented
		fmt.Println(err)
	}
}
