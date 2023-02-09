package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/etc/example/greeter"
	"github.com/spacegrower/watermelon/infra"
	"github.com/spacegrower/watermelon/infra/wlog"
)

func TestXxx(t *testing.T) {
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

	newClientConn := infra.NewClientConn()
	cc, err := newClientConn(greeter.Greeter_ServiceDesc.ServiceName,
		newClientConn.WithNamespace("test"),
		newClientConn.WithGrpcOptions(grpc.WithInsecure()),
		newClientConn.WithRegion("test"))
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
}
