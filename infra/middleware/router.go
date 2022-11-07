package middleware

import (
	"container/list"
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
}

type RouterGroup interface {
	Use(m ...Middleware)
	Group() RouterGroup
	Handler(methods ...interface{})
}

type RouterV1 struct {
	list list.List
}

func (r *RouterV1) Deep(ctx context.Context) error {
	if r.list.Front() != nil {
		return next(ctx, r.list.Front())
	}
	return nil
}

func next(ctx context.Context, ele *list.Element) error {
	if ele == nil {
		return handler(ctx)
	}
	r, ok := ele.Value.(*router)
	if ok {
		SetInto(ctx, definition.RouterIndex{}, r.index)
		SetInto(ctx, definition.CurrentRouterKey{}, ele)
		if err := r.handler(ctx); err != nil {
			return err
		}

		if GetFrom(ctx, definition.RouterIndex{}).(int) == r.index {
			return next(ctx, ele.Next())
		}
	}
	return nil
}

type router struct {
	index   int
	handler func(ctx context.Context) error
}

// func (r *router) IsNil() bool {
// 	return r == nil
// }

// func (r *router) Next() Router {
// 	return r.next
// }

// func (r *router) Deep(ctx context.Context) error {
// 	if r.IsNil() {
// 		return handler(ctx)
// 	}
// 	SetInto(ctx, definition.RouterIndex{}, r.index)
// 	SetInto(ctx, definition.CurrentRouterKey{}, r)
// 	if err := r.handler(ctx); err != nil {
// 		return err
// 	}

// 	if r.Next().IsNil() || GetFrom(ctx, definition.RouterIndex{}).(int) == r.index {
// 		return r.next.Deep(ctx)
// 	}
// 	return nil
// }

func handler(ctx context.Context) error {
	var (
		resp any
		err  error
	)
	switch GetGrpcRequestTypeFrom(ctx) {
	case cst.UnaryRequest:
		resp, err = GetUnaryHandlerFrom(ctx)(ctx, GetRequestFrom(ctx))
		SetInto(ctx, definition.ResponseKey{}, resp)
	case cst.StreamRequest:
		err = GetStreamHandlerFrom(ctx)()
	}
	if err != nil {
		return err
	}
	return nil
}

type routerGroup struct {
	router      *list.List
	index       int
	locker      sync.Mutex
	ExistRouter func(key string) bool
	AddRouter   func(key string, router Router)
}

func NewRouterGroup(existRouter func(key string) bool, addRouter func(key string, router Router)) RouterGroup {
	return &routerGroup{
		router:      list.New(),
		ExistRouter: existRouter,
		AddRouter:   addRouter,
	}
}

func (r *routerGroup) Use(m ...Middleware) {
	if r.router == nil {
		r.router = list.New()
	}
	r.locker.Lock()
	for _, v := range m {
		rr := &router{
			index:   r.index,
			handler: v,
		}
		r.router.PushBack(rr)
		r.index++
	}
	r.locker.Unlock()
}

func (r *routerGroup) Group() RouterGroup {
	r.locker.Lock()
	defer r.locker.Unlock()
	return &routerGroup{
		router:      &(*r.router),
		ExistRouter: r.ExistRouter,
		AddRouter:   r.AddRouter,
	}
}

func (r *routerGroup) Handler(methods ...interface{}) {
	r.locker.Lock()
	defer r.locker.Unlock()

	if r.router == nil {
		return
	}

	for _, method := range methods {
		funcName := getGrpcFunctionName(method)
		if funcName == "" {
			wlog.Panic("router: failed to patch handler function name")
		}

		if exist := r.ExistRouter(funcName); exist {
			wlog.Panic("router: duplic handler")
		}

		r.AddRouter(funcName, &RouterV1{*r.router})
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

func copyRouter(r *router) *router {
	if r == nil {
		return r
	}
	nr := new(router)
	*nr = *r
	return nr
}
