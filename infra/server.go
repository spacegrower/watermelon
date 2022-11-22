package infra

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/graceful"
	wctx "github.com/spacegrower/watermelon/infra/internal/context"
	"github.com/spacegrower/watermelon/infra/internal/preset"
	"github.com/spacegrower/watermelon/infra/middleware"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/version"
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
	orgid     string
	region    string
	namespace string
	name      string
	address   string
	port      string
	tags      map[string]string
}

func (s *server) interceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp interface{}, err error) {

		c := wctx.Wrap(ctx)

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

type fakeServerStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (s *fakeServerStream) Context() context.Context {
	return s.ctx
}

func (s *server) streamInterceptor() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		c := wctx.Wrap(ss.Context())

		preset.SetFullMethodInto(c, info.FullMethod)
		preset.SetGrpcRequestTypeInto(c, definition.StreamRequest)
		preset.SetStreamHandlerInto(c, func() error {
			return handler(srv, &fakeServerStream{
				ctx:          c,
				ServerStream: ss,
			})
		})

		method := utils.PathBase(info.FullMethod)

		if router, exist := s.routers[method]; exist {
			if err := router.Deep(c); err != nil {
				return err
			}
			return nil
		}
		return handler(srv, ss)
	}
}

type Option func(s *server)

func (*Server) WithOrg(id string) Option {
	return func(s *server) {
		s.orgid = id
	}
}

func (*Server) WithNamespace(ns string) Option {
	return func(s *server) {
		s.serverInfo.namespace = ns
	}
}

func (*Server) WithRegion(region string) Option {
	return func(s *server) {
		s.serverInfo.region = region
	}
}

func (*Server) WithName(name string) Option {
	return func(s *server) {
		s.serverInfo.name = name
	}
}

func (*Server) WithAddress(addr string) Option {
	return func(s *server) {
		s.serverInfo.address = addr
	}
}

func (*Server) WithServiceRegister(r register.ServiceRegister) Option {
	return func(s *server) {
		s.registry = r
	}
}

func (*Server) WithTags(tags map[string]string) Option {
	return func(s *server) {
		s.tags = tags
	}
}

func (s *Server) WithHttpServer(srv *http.Server) Option {
	return func(s *server) {
		s.httpServer = srv
	}
}

func (s *Server) WithGrpcServerOptions(opts ...grpc.ServerOption) Option {
	return func(s *server) {
		s.grpcServerOptions = opts
	}
}

func newServer(register func(srv *grpc.Server), opts ...Option) *server {
	s := &server{
		serverInfo: serverInfo{
			orgid:     "default",
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

	baseGrpcServerOptions := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(s.interceptor()),
		grpc.ChainStreamInterceptor(s.streamInterceptor())}
	s.grpcServerOptions = append(baseGrpcServerOptions, s.grpcServerOptions...)

	for _, opt := range opts {
		opt(s)
	}

	// 有些场景可能不需要服务注册
	// if s.registry == nil {
	// 	s.registry = etcd.MustSetupEtcdRegister()
	// }

	addrAndPort := strings.Split(s.address, ":")
	s.address = addrAndPort[0]
	if s.address == "" {
		var err error
		if s.address, err = utils.GetHostIP(); err != nil {
			panic(err)
		}
	}
	if len(addrAndPort) == 2 {
		var err error
		if _, err = strconv.Atoi(addrAndPort[1]); err != nil {
			panic(fmt.Sprintf("wrong port %s, %s", addrAndPort[1], err.Error()))
		}
		s.port = addrAndPort[1]
	}

	s.grpcServer = grpc.NewServer(s.grpcServerOptions...)
	register(s.grpcServer)

	return s
}

func (s *server) Serve(notifications ...chan struct{}) error {
	l, err := net.Listen("tcp", ":"+s.port)
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
			graceful.RegisterPreShutDownHandlers(func() {
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
		wlog.Info("start grpc server")
		_ = s.grpcServer.Serve(grpcListener)
	}()

	time.Sleep(time.Millisecond * 100)
	if err = s.registerServer(s.address, addr.Port); err != nil {
		panic(err)
	}

	for _, note := range notifications {
		close(note)
	}

	graceful.RegisterPreShutDownHandlers(func() {
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

func (s *server) GetServiceName() string {
	for name := range s.grpcServer.GetServiceInfo() {
		return name
	}
	return ""
}

func (s *server) GetServiceMethods() []grpc.MethodInfo {
	for _, info := range s.grpcServer.GetServiceInfo() {
		return info.Methods
	}
	return nil
}

func (s *server) registerServer(host string, port int) error {
	if s.registry == nil {
		wlog.Warn("start server without register")
		return nil
	}

	metaData := register.NodeMeta{
		OrgID:        s.orgid,
		Region:       s.region,
		Namespace:    s.namespace,
		ServiceName:  s.GetServiceName(),
		Host:         host,
		Port:         port,
		Weight:       100,
		Tags:         s.tags,
		Methods:      nil,
		Runtime:      runtime.Version(),
		Version:      version.Version,
		RegisterTime: time.Now().Unix(),
	}

	for _, v := range s.GetServiceMethods() {
		metaData.Methods = append(metaData.Methods, register.GrpcMethodInfo{
			Name:           v.Name,
			IsClientStream: v.IsClientStream,
			IsServerStream: v.IsServerStream,
		})
	}

	s.registry.Init(metaData)
	return s.registry.Register()
}
