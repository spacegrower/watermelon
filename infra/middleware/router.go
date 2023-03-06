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
	"github.com/spacegrower/watermelon/infra/utils"
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
	return next(ctx, r.list.Front())
}

func next(ctx context.Context, ele *list.Element) error {
	if ele == nil {
		SetInto(ctx, definition.CurrentRouterKey{}, nil)
		return handler(ctx)
	}

	if r, ok := ele.Value.(*router); ok {
		SetInto(ctx, definition.CurrentRouterKey{}, ele)
		if err := r.handler(ctx); err != nil {
			return err
		}
		if currentRouter, ok := GetFrom(ctx, definition.CurrentRouterKey{}).(interface {
			Next() *list.Element
		}); ok {
			return next(ctx, currentRouter.Next())
		}
	}
	return nil
}

type router struct {
	index   int
	handler func(ctx context.Context) error
}

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
	serviceName string
	router      *list.List
	index       int
	locker      sync.Mutex
	ExistRouter func(key string) bool
	AddRouter   func(key string, router Router)
}

func (r *routerGroup) routerName(method interface{}) string {
	name := getGrpcFunctionName(method)
	if name != "" && r.serviceName != "" {
		return utils.PathJoin(r.serviceName, name)
	}
	return name
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

// GroupWithServiceName multi service used to distinguish different services of methods with the same name
func (r *routerGroup) GroupWithServiceName(name string) RouterGroup {
	r.locker.Lock()
	defer r.locker.Unlock()
	if name != "" && !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	n := &routerGroup{
		serviceName: name,
		router:      list.New(),
		ExistRouter: r.ExistRouter,
		AddRouter:   r.AddRouter,
	}
	if r.router != nil {
		n.router.PushBackList(r.router)
	}
	return n
}

func (r *routerGroup) Group() RouterGroup {
	return r.GroupWithServiceName("")
}

func (r *routerGroup) Handler(methods ...interface{}) {
	r.locker.Lock()
	defer r.locker.Unlock()

	if r.router == nil {
		return
	}

	for _, method := range methods {
		routerName := r.routerName(method)
		if routerName == "" {
			wlog.Panic("router: failed to patch handler function name")
		}

		if exist := r.ExistRouter(routerName); exist {
			wlog.Panic("router: duplic handler")
		}

		router := &RouterV1{}
		router.list.PushBackList(r.router)
		r.AddRouter(routerName, router)
	}
}

func getGrpcFunctionName(i interface{}) string {
	if str, ok := i.(string); ok {
		return str
	}
	sep := rune('.')
	fn := runtime.FuncForPC(reflect.ValueOf(i).Pointer()).Name()
	fields := strings.FieldsFunc(fn, func(s rune) bool {
		return sep == s
	})

	if len(fields) > 0 {
		return strings.Split(fields[len(fields)-1], "-")[0]
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
