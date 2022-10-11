package infra

import (
	"context"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/graceful"
	wmctx "github.com/spacegrower/watermelon/infra/internal/context"
	"github.com/spacegrower/watermelon/infra/internal/preset"
	"github.com/spacegrower/watermelon/infra/middleware"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
)

type ServerConfig struct {
	Namespace string
	ListenOn  string
}

type server struct {
	serverInfo
	sync.Mutex

	grpcServer        *grpc.Server
	grpcServerOptions []grpc.ServerOption

	httpServer *http.Server

	registry register.ServiceRegister

	middleware.RouterGroup
	routers map[string]middleware.Router
}

type serverInfo struct {
	region    string
	namespace string
	name      string
	address   string
	port      int
	tags      map[string]string
}

func (s *server) interceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp interface{}, err error) {

		c := wmctx.Wrap(ctx)

		preset.SetFullMethodInto(c, info.FullMethod)
		preset.SetRequestInto(c, req)
		preset.SetGrpcRequestTypeInto(c, definition.UnaryRequest)
		preset.SetUnaryHandlerInto(c, handler)

		method := utils.PathBase(info.FullMethod)

		if router, exist := s.routers[method]; exist {
			if err := router.Deep(c); err != nil {
				return nil, err
			}
			return middleware.GetResponseFrom(c), nil
		}
		return handler(c, req)
	}
}

type Option func(s *server)

func WithNamespace(ns string) Option {
	return func(s *server) {
		s.serverInfo.namespace = ns
	}
}

func WithRegion(region string) Option {
	return func(s *server) {
		s.serverInfo.region = region
	}
}

func WithName(name string) Option {
	return func(s *server) {
		s.serverInfo.name = name
	}
}

func WithAddress(addr string) Option {
	return func(s *server) {
		s.serverInfo.address = addr
	}
}

func WithServiceRegister(r register.ServiceRegister) Option {
	return func(s *server) {
		s.registry = r
	}
}

func NewServer(register func(srv *grpc.Server), opts ...Option) *server {
	s := &server{
		serverInfo: serverInfo{
			region:    "default",
			namespace: "default",
			name:      "default",
		},
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

	baseGrpcServerOptions := []grpc.ServerOption{grpc.UnaryInterceptor(s.interceptor())}
	s.grpcServerOptions = append(baseGrpcServerOptions, s.grpcServerOptions...)

	for _, opt := range opts {
		opt(s)
	}

	if s.address == "" {
		var err error
		if s.address, err = utils.GetHostIP(); err != nil {
			panic(err)
		}
		s.address += ":"
	}

	s.grpcServer = grpc.NewServer(s.grpcServerOptions...)
	register(s.grpcServer)

	return s
}

func (s *server) Serve(notifications ...chan struct{}) error {
	// create the main listener
	l, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}

	addr, err := net.ResolveTCPAddr(l.Addr().Network(), l.Addr().String())
	if err != nil {
		return err
	}

	m := cmux.New(l)

	if s.grpcServer == nil {
		wlog.Panic("grpc server not set")
	}

	if s.httpServer != nil {
		httpListener := m.Match(cmux.HTTP1Fast())
		go func() {
			wlog.Info("start http serve")
			graceful.RegisterShutDownHandlers(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
				defer cancel()
				s.httpServer.Shutdown(ctx)
				wlog.Info("http server shutdown now")
			})
			_ = s.httpServer.Serve(httpListener)
		}()
	}

	grpcListener := m.Match(cmux.Any())
	go func() {
		wlog.Info("start grpc serve")
		_ = s.grpcServer.Serve(grpcListener)
	}()

	if err = s.registerServer(addr); err != nil {
		panic(err)
	}

	for _, note := range notifications {
		close(note)
	}

	graceful.RegisterShutDownHandlers(func() {
		s.grpcServer.GracefulStop()
		wlog.Info("grpc server shutdown now")
	})
	// Start serving!
	return m.Serve()
}

// RunUntil start server and shutdown until receive signals
func (s *server) RunUntil(signals ...os.Signal) {
	ctx, cancel := context.WithCancel(utils.NewContextWithSignal(signals...))
	go func() {
		defer cancel()
		if err := s.Serve(); err != nil {
			wlog.Error("watermelon server is shutdown", zap.Error(err))
		}
	}()

	<-ctx.Done()
	graceful.ShutDown()
}

func (s *server) registerServer(addr *net.TCPAddr) error {
	if s.registry == nil {
		return nil
	}

	s.registry.Init(s.grpcServer, s.region, s.namespace, addr.IP.String(), addr.Port, s.tags)
	return s.registry.Register()
}
