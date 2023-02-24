package firemelon_test

import (
	"context"
	"syscall"
	"testing"
	"time"

	"github.com/spacegrower/watermelon/etc/example/book"
	"github.com/spacegrower/watermelon/infra"
	"github.com/spacegrower/watermelon/infra/wlog"
	clientv3 "go.etcd.io/etcd/client/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	firemelon "github.com/spacegrower/watermelon/example/customize"
)

func Init() {
	// install logger
	wlog.SetGlobalLogger(wlog.NewLogger(&wlog.Config{
		Name:  "example/customize",
		Level: wlog.DebugLevel,
	}))

	// register etcd client
	if err := infra.RegisterEtcdClient(clientv3.Config{
		Endpoints: []string{"localhost:2379"},
	}); err != nil {
		panic(err)
	}
}

type BookSrv struct {
	book.UnimplementedBookServer
}

func (s *BookSrv) GetBook(ctx context.Context, req *book.GetBookRequest) (*book.GetBookReply, error) {
	return &book.GetBookReply{
		Message: "got book: " + req.Name,
	}, nil
}

func TestServer(t *testing.T) {
	Init()

	newServer := firemelon.NewServer()
	srv := newServer(func(srv *grpc.Server) {
		book.RegisterBookServer(srv, &BookSrv{})
	}, newServer.WithSystem("BookSystem"),
		newServer.WithRegion("local"),
		newServer.WithServiceRegister(firemelon.MustSetupEtcdRegister()))

	go srv.RunUntil(syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	time.Sleep(time.Second * 3) // sleep to wati server start
	newClient := firemelon.NewClientConn()
	cc, err := newClient(book.Book_ServiceDesc.ServiceName,
		newClient.WithRegion("local"),
		newClient.WithSystem("BookSystem"),
		newClient.WithGrpcDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		t.Fatal(err)
	}

	client := book.NewBookClient(cc)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	resp, err := client.GetBook(ctx, &book.GetBookRequest{
		Name: "Hello",
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Message != "got book: Hello" {
		t.Fatal("receive wrong message", resp.Message)
	}
	t.Log("success")
}
