package infra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"testing"

	"github.com/spacegrower/watermelon/infra/definition"
	wmctx "github.com/spacegrower/watermelon/infra/internal/context"
	"github.com/spacegrower/watermelon/infra/internal/preset"
	"github.com/spacegrower/watermelon/infra/middleware"
	"google.golang.org/grpc"
)

type TestForGetName struct{}

func (*TestForGetName) ForGetName() {}
func Test_getGrpcFunctionName(t *testing.T) {
	a := &TestForGetName{}
	fn := runtime.FuncForPC(reflect.ValueOf(a.ForGetName).Pointer()).Name()
	t.Log(fn)
	t.Log()
}

func Test_RandomListen(t *testing.T) {
	l, err := net.Listen("tcp", ":")
	if err != nil {
		t.Fatal(err)
	}

	addr, err := net.ResolveTCPAddr(l.Addr().Network(), l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	t.Log(addr.IP, addr.Port)
}

func Test_Server(t *testing.T) {
	s := &server{
		routers: make(map[string]middleware.Router),
	}
	s.RouterGroup = middleware.NewRouterGroup(func(key string) bool {
		s.Lock()
		_, exist := s.routers[key]
		s.Unlock()
		return exist
	}, func(key string, router middleware.Router) {
		s.Lock()
		s.routers[key] = router
		s.Unlock()
	})

	var str string

	s.Use(func(ctx context.Context) error {
		str += "1"
		return nil
	})

	g := s.Group()
	g.Use(func(ctx context.Context) error {
		middleware.Next(ctx)
		str += "2"
		return nil
	}, func(ctx context.Context) error {
		middleware.Next(ctx)
		str += "3"
		return nil
	}, func(ctx context.Context) error {
		middleware.SetInto(ctx, "verify", "123456")
		str += "4"
		return nil
	})

	var testFunc grpc.UnaryHandler = func(ctx context.Context, req interface{}) (interface{}, error) {
		str += "test"
		if middleware.GetFrom(ctx, "verify").(string) != "123456" {
			t.Fatal("bad middleware")
		}
		return nil, nil
	}

	g.Handler(testFunc)

	ctx := wmctx.Wrap(context.Background())
	preset.SetGrpcRequestTypeInto(ctx, definition.UnaryRequest)
	preset.SetUnaryHandlerInto(ctx, testFunc)

	for _, v := range s.routers {
		v.Deep(ctx)
	}

	if str != "14test32" {
		t.Fatal(str)
	}
	t.Log("successful")
}

func Test_ServerMiddlewareErrorReturn(t *testing.T) {
	s := &server{
		routers: make(map[string]middleware.Router),
	}
	s.RouterGroup = middleware.NewRouterGroup(func(key string) bool {
		s.Lock()
		_, exist := s.routers[key]
		s.Unlock()
		return exist
	}, func(key string, router middleware.Router) {
		s.Lock()
		fmt.Println(key)
		s.routers[key] = router
		s.Unlock()
	})

	errEp := errors.New("test error")
	str := ""

	s.Use(func(ctx context.Context) error {
		return nil
	})

	g := s.Group()
	g.Use(func(ctx context.Context) error {

		if err := middleware.Next(ctx); err != nil {
			return err
		}
		str = "1"
		return nil
	}, func(ctx context.Context) error {
		if err := middleware.Next(ctx); err != nil {
			return err
		}
		return nil
	}, func(ctx context.Context) error {
		middleware.SetInto(ctx, "verify", "123456")
		return errEp
	})

	var testFunc grpc.UnaryHandler = func(ctx context.Context, req interface{}) (interface{}, error) {
		if middleware.GetFrom(ctx, "verify").(string) != "123456" {
			t.Fatal("bad middleware")
		}
		return nil, nil
	}

	s.Use(func(ctx context.Context) error {
		panic("wrong")
	})

	g.Handler(testFunc)

	ctx := wmctx.Wrap(context.Background())
	preset.SetGrpcRequestTypeInto(ctx, definition.UnaryRequest)
	preset.SetUnaryHandlerInto(ctx, testFunc)

	for _, v := range s.routers {
		if err := v.Deep(ctx); err != nil {
			if err != errEp {
				t.Fatal(err)
			}
		}
	}

	if str != "" {
		t.Fatal("failed")
	}

	t.Log("successful")
}
