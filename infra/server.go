package infra

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soheilhy/cmux"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	_ "github.com/spacegrower/watermelon/infra/codec"
	"github.com/spacegrower/watermelon/infra/definition"
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

type SrvInfo[T interface {
	WithMeta(register.NodeMeta) T
}] struct {
	address           string
	CustomInfo        T
	grpcServerOptions []grpc.ServerOption
	httpServer        *http.Server
	registry          register.ServiceRegister[T]
}

type Srv[T interface {
	WithMeta(register.NodeMeta) T
}] struct {
	ctx        context.Context
	cancelFunc func()

	*SrvInfo[T]
	port  string
	addr  *net.TCPAddr
	mutex sync.Mutex

	grpcServer *grpc.Server

	middleware.RouterGroup
	routers map[string]middleware.Router

	shutdownFunc []func()
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

func (s *Srv[T]) Port() int {
	return s.addr.Port
}

func (s *Srv[T]) Addr() string {
	return s.addr.IP.String()
}

func (s *Srv[T]) interceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp interface{}, err error) {

		c := wctx.Wrap(ctx)

		preset.SetFullMethodInto(c, info.FullMethod)
		preset.SetRequestInto(c, req)
		preset.SetGrpcRequestTypeInto(c, definition.UnaryRequest)
		preset.SetUnaryHandlerInto(c, handler)

		var (
			router middleware.Router
			exist  bool
		)
		if router, exist = s.routers[info.FullMethod]; !exist {
			if router, exist = s.routers[filepath.Base(info.FullMethod)]; !exist {
				return handler(c, req)
			}
		}

		if err := router.Deep(c); err != nil {
			return nil, err
		}
		return middleware.GetResponseFrom(c), nil
	}
}

type fakeServerStream struct {
	ctx context.Context
	grpc.ServerStream
}

func (s *fakeServerStream) Context() context.Context {
	return s.ctx
}

func (s *Srv[T]) streamInterceptor() grpc.StreamServerInterceptor {
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

		var (
			router middleware.Router
			exist  bool
		)
		if router, exist = s.routers[info.FullMethod]; !exist {
			if router, exist = s.routers[filepath.Base(info.FullMethod)]; !exist {
				return handler(srv, ss)
			}
		}

		if err := router.Deep(c); err != nil {
			return err
		}
		return nil
	}
}

type Option[T interface {
	WithMeta(register.NodeMeta) T
}] func(s *SrvInfo[T])

// func (*Server) WithName(name string) Option {
// 	return func(s *server) {
// 		s.serverInfo.name = name
// 	}
// }

func WithAddress[T interface {
	WithMeta(register.NodeMeta) T
}](addr string) Option[T] {
	return func(s *SrvInfo[T]) {
		s.address = addr
	}
}

func WithServiceRegister[T interface {
	WithMeta(register.NodeMeta) T
}](r register.ServiceRegister[T]) Option[T] {
	return func(s *SrvInfo[T]) {
		s.registry = r
	}
}

func WithHttpServer[T interface {
	WithMeta(register.NodeMeta) T
}](srv *http.Server) Option[T] {
	return func(s *SrvInfo[T]) {
		s.httpServer = srv
	}
}

func WithGrpcServerOptions[T interface {
	WithMeta(register.NodeMeta) T
}](opts ...grpc.ServerOption) Option[T] {
	return func(s *SrvInfo[T]) {
		s.grpcServerOptions = opts
	}
}

func NewServer[T interface {
	WithMeta(register.NodeMeta) T
}](register func(srv *grpc.Server), opts ...Option[T]) *Srv[T] {
	s := &Srv[T]{
		SrvInfo: new(SrvInfo[T]),
		routers: make(map[string]middleware.Router),
	}

	s.ctx, s.cancelFunc = context.WithCancel(context.Background())

	s.RouterGroup = middleware.NewRouterGroup(func(key string) bool {
		s.mutex.Lock()
		_, exist := s.routers[key]
		s.mutex.Unlock()
		return exist
	}, func(key string, router middleware.Router) {
		s.mutex.Lock()
		s.routers[key] = router
		s.mutex.Unlock()
	})

	baseGrpcServerOptions := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(s.interceptor()),
		grpc.ChainStreamInterceptor(s.streamInterceptor())}
	s.grpcServerOptions = append(baseGrpcServerOptions, s.grpcServerOptions...)

	for _, opt := range opts {
		opt(s.SrvInfo)
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
	reflection.Register(s.grpcServer)

	if len(s.grpcServer.GetServiceInfo()) == 0 {
		wlog.Panic("cannot register grpc service into grpc server")
	}

	return s
}

func (s *Srv[T]) autoSetupAvailableMethods() {
	for srvName, srvInfo := range s.grpcServer.GetServiceInfo() {
		for _, method := range srvInfo.Methods {
			if _, exist := s.routers[method.Name]; !exist {
				fullMethds := utils.PathJoin(srvName, method.Name)
				if _, exist := s.routers[fullMethds]; !exist {
					s.Handler(fullMethds)
				}
			}
		}
	}
}

func (s *Srv[T]) serve() error {
	s.autoSetupAvailableMethods()

	l, err := net.Listen("tcp", fmt.Sprintf("%s:%s", s.address, s.port))
	if err != nil {
		return err
	}

	addr, err := net.ResolveTCPAddr(l.Addr().Network(), l.Addr().String())
	if err != nil {
		return err
	}

	s.addr = addr

	m := cmux.New(l)

	if s.grpcServer == nil {
		wlog.Panic("grpc server not set")
	}

	if s.httpServer != nil {
		httpListener := m.Match(cmux.HTTP1Fast())
		go func() {
			wlog.Info("start http serve")
			s.shutdownFunc = append(s.shutdownFunc, func() {
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
	if err = s.registerServer(addr.IP.String(), addr.Port); err != nil {
		panic(err)
	}

	s.shutdownFunc = append(s.shutdownFunc, func() {
		if s.registry != nil {
			s.registry.Close()
		}
		m.Close()
		s.grpcServer.GracefulStop()
		wlog.Info("grpc server shutdown now")
	})

	// Start serving!
	return m.Serve()
}

// RunUntil start server and shutdown until receive signals
func (s *Srv[T]) RunUntil(signals ...os.Signal) {
	ctx, cancel := context.WithCancel(utils.NewContextWithSignal(s.ctx, signals...))
	go func() {
		defer cancel()
		if err := s.serve(); err != nil {
			wlog.Error("watermelon server is shutdown", zap.Error(err))
		}
	}()

	<-ctx.Done()
	for _, f := range s.shutdownFunc {
		f()
	}
}

func (s *Srv[T]) ShutDown() {
	s.cancelFunc()
}

func (s *Srv[T]) registerServer(host string, port int) error {
	if s.registry == nil {
		wlog.Warn("start server without register")
		return nil
	}

	metaData := register.NodeMeta{
		// OrgID:        s.orgid,
		// Region:       s.region,
		// Namespace:    s.namespace,
		Host: host,
		Port: port,
		// Weight:       100,
		// Tags:         s.tags,
		Runtime: runtime.Version(),
		Version: version.Version,
	}

	for serviceName, serviceInfo := range s.grpcServer.GetServiceInfo() {
		if strings.HasPrefix(serviceName, "grpc.reflection") {
			// grpc reflection is a sidecar reflection methods, can not register
			continue
		}
		metaData.ServiceName = serviceName
		for _, method := range serviceInfo.Methods {
			metaData.GrpcMethods = append(metaData.GrpcMethods, register.GrpcMethodInfo{
				Name:           method.Name,
				IsClientStream: method.IsClientStream,
				IsServerStream: method.IsServerStream,
			})
		}

		s.registry.Append(s.CustomInfo.WithMeta(metaData))
	}

	return s.registry.Register()
}
