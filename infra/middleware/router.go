package middleware

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"sync"

	cst "github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/internal/definition"
	"github.com/spacegrower/watermelon/infra/wlog"
)

type Router interface {
	Deep(ctx context.Context) error
	Next() Router
	IsNil() bool
}

type RouterGroup interface {
	Use(m ...Middleware)
	Group() RouterGroup
	Handler(methods ...interface{})
}

type router struct {
	index   int
	next    *router
	handler func(ctx context.Context) error
}

func (r *router) IsNil() bool {
	return r == nil
}

func (r *router) Next() Router {
	return r.next
}

func (r *router) Deep(ctx context.Context) error {
	if r.IsNil() {
		return handler(ctx)
	}
	SetInto(ctx, definition.RouterIndex{}, r.index)
	SetInto(ctx, definition.CurrentRouterKey{}, r)
	if err := r.handler(ctx); err != nil {
		return err
	}
	if r.Next().IsNil() || GetFrom(ctx, definition.RouterIndex{}).(int) == r.index {
		return r.next.Deep(ctx)
	}
	return nil
}

func handler(ctx context.Context) error {
	var (
		resp any
		err  error
	)
	switch GetGrpcRequestTypeFrom(ctx) {
	case cst.UnaryRequest:
		resp, err = GetUnaryHandlerFrom(ctx)(ctx, GetRequestFrom(ctx))
	case cst.StreamRequest:
		// todo stream
		panic("unsupport stream handler")
	}
	if err != nil {
		return err
	}
	SetInto(ctx, definition.ResponseKey{}, resp)
	return nil
}

type routerGroup struct {
	router       *router
	latestRouter *router
	locker       sync.Mutex
	ExistRouter  func(key string) bool
	AddRouter    func(key string, router Router)
}

func NewRouterGroup(existRouter func(key string) bool, addRouter func(key string, router Router)) RouterGroup {
	return &routerGroup{
		ExistRouter: existRouter,
		AddRouter:   addRouter,
	}
}

func (r *routerGroup) Use(m ...Middleware) {
	r.locker.Lock()
	for _, v := range m {
		rr := &router{
			handler: v,
		}
		if r.latestRouter == nil {
			r.router = rr
			r.latestRouter = rr
			continue
		}
		rr.index = r.latestRouter.index + 1
		r.latestRouter.next = rr
		r.latestRouter = rr
	}
	r.locker.Unlock()
}

func (r *routerGroup) Group() RouterGroup {
	r.locker.Lock()
	defer r.locker.Unlock()
	return &routerGroup{
		router:       &(*r.router),
		latestRouter: &(*r.router),
		ExistRouter:  r.ExistRouter,
		AddRouter:    r.AddRouter,
	}
}

func (r *routerGroup) Handler(methods ...interface{}) {
	r.locker.Lock()
	defer r.locker.Unlock()
	for _, method := range methods {

		funcName := getGrpcFunctionName(method)
		if funcName == "" {
			wlog.Panic("router: failed to patch handler function name")
		}

		if exist := r.ExistRouter(funcName); exist {
			wlog.Panic("router: duplic handler")
		}

		r.AddRouter(funcName, &(*r.router))

	}
}

func getGrpcFunctionName(i interface{}) string {
	var (
		seps = []rune{'.'}
	)
	fn := runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()

	fields := strings.FieldsFunc(fn, func(sep rune) bool {
		for _, s := range seps {
			if sep == s {
				return true
			}
		}
		return false
	})

	if size := len(fields); size > 0 {
		return fields[size-1]
	}
	return ""
}
