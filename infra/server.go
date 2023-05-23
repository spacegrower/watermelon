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
	address           []Address
	CustomInfo        T
	grpcServerOptions []grpc.ServerOption
	httpServer        *http.Server
	registry          register.ServiceRegister[T]
}

type Address struct {
	ListenAddress string // exp: 10.0.0.1 or 10.0.0.1:12345 or :12345 (used host-ip:12345)
	// register-address will be equal listen-address when it is empty
	//
	// The registration address does not need to provide a port number.
	// Even if it is provided, it will be replaced with the port number
	// used by the listening address, unless a valid URL address is provided.
	// If the provided URL address needs to automatically fill in the port
	// number information used by the listener, you can use `{port}`` as a
	// template for the port number filling.
	RegisterAddress string
	address         string
	port            int
}

type Srv[T interface {
	WithMeta(register.NodeMeta) T
}] struct {
	ctx        context.Context
	cancelFunc func()

	*SrvInfo[T]
	port  string
	addr  []*net.TCPAddr
	mutex sync.Mutex

	grpcServer *grpc.Server

	middleware.RouterGroup
	routers map[string]middleware.Router

	closer      closer
	serverGroup sync.WaitGroup
}

type closer struct {
	shutdownFunc    []func()
	shutdownContext context.Context
	done            func()
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

// Addrs It will always return nil before the service is started
func (s *Srv[T]) Addrs() []*net.TCPAddr {
	return s.addr
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

func WithAddress[T interface {
	WithMeta(register.NodeMeta) T
}](addrs []Address) Option[T] {
	return func(s *SrvInfo[T]) {
		s.address = addrs
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
		s.grpcServerOptions = append(s.grpcServerOptions, opts...)
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
	s.closer = closer{}
	s.closer.shutdownContext, s.closer.done = context.WithCancel(context.Background())

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

	// This may not require service registration in some scenarios.
	// if s.registry == nil {
	// 	s.registry = etcd.MustSetupEtcdRegister()
	// }

	s.mustResolvServiceAddress()

	s.grpcServer = grpc.NewServer(s.grpcServerOptions...)

	register(s.grpcServer)
	reflection.Register(s.grpcServer)

	if len(s.grpcServer.GetServiceInfo()) == 0 {
		wlog.Panic("cannot register grpc service into grpc server")
	}

	return s
}

func (s *Srv[T]) mustResolvServiceAddress() {
	var err error
	for i, address := range s.address {
		addrAndPort := strings.Split(address.ListenAddress, ":")
		s.address[i].address = addrAndPort[0]
		if s.address[i].address == "" {
			if s.address[i].address, err = utils.GetHostIP(); err != nil {
				panic(err)
			}
		}
		if len(addrAndPort) == 2 {
			if _, err = strconv.Atoi(addrAndPort[1]); err != nil {
				panic(fmt.Sprintf("wrong port %s, %s", addrAndPort[1], err.Error()))
			}
			if s.address[i].port, err = strconv.Atoi(addrAndPort[1]); err != nil {
				panic(fmt.Sprintf("address[%d].port can not parsed to int, %s", i, err.Error()))
			}
		}
	}

	if len(s.address) == 0 {
		s.address = append(s.address, Address{})
	}
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
	if s.grpcServer == nil && s.httpServer == nil {
		wlog.Panic("no server set")
	}

	s.autoSetupAvailableMethods()

	duplicateFilter := make(map[string]struct{})
	for i, addr := range s.address {
		if len(s.address) > 1 && addr.ListenAddress == "" && addr.RegisterAddress == "" {
			continue
		}
		l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", addr.address, addr.port))
		if err != nil {
			return err
		}

		netAddr, err := net.ResolveTCPAddr(l.Addr().Network(), l.Addr().String())
		if err != nil {
			return err
		}

		if _, exist := duplicateFilter[netAddr.IP.String()]; exist {
			l.Close()
			continue
		}
		duplicateFilter[netAddr.IP.String()] = struct{}{}

		s.addr = append(s.addr, netAddr)

		m := cmux.New(l)
		if s.httpServer != nil {
			httpListener := m.Match(cmux.HTTP1Fast())
			go func(l net.Listener) {
				wlog.Info("start http server on " + l.Addr().String())
				_ = s.httpServer.Serve(l)
			}(httpListener)
		}

		if s.grpcServer != nil {
			grpcListener := m.Match(cmux.Any())
			go func(l net.Listener) {
				wlog.Info("start grpc server on " + l.Addr().String())
				_ = s.grpcServer.Serve(l)
			}(grpcListener)
		}

		s.serverGroup.Add(1)

		s.closer.shutdownFunc = append(s.closer.shutdownFunc, func() {
			m.Close()
			s.serverGroup.Done()
		})
		go m.Serve()

		registerPort := netAddr.Port
		ra := strings.Split(addr.RegisterAddress, ":")
		if len(ra) == 2 && ra[1] != "{port}" {
			if registerPort, err = strconv.Atoi(ra[1]); err != nil {
				panic(fmt.Sprintf("register address[%d].port can not parsed to int, %s", i, err.Error()))
			}
		}

		if ra[0] == "" {
			ra[0] = netAddr.IP.String()
		}

		if err = s.registerServer(ra[0], registerPort); err != nil {
			panic(err)
		}
	}
	duplicateFilter = nil

	if s.httpServer != nil {
		s.closer.shutdownFunc = append(s.closer.shutdownFunc, func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
			defer cancel()
			s.httpServer.Shutdown(ctx)
			wlog.Info("http server shutdown now")
		})
	}

	if s.grpcServer != nil {
		s.closer.shutdownFunc = append(s.closer.shutdownFunc, func() {
			if s.registry != nil {
				s.registry.Close()
			}
			s.grpcServer.GracefulStop()
			wlog.Info("grpc server shutdown now")
		})
	}

	if s.registry != nil {
		time.Sleep(time.Millisecond * 100)
		if err := s.registry.Register(); err != nil {
			return err
		}
	}

	// Start serving!
	s.serverGroup.Wait()
	return nil
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
	s.ShutDown()
}

func (s *Srv[T]) ShutDown() {
	s.cancelFunc()
	go func() {
		for _, f := range s.closer.shutdownFunc {
			f()
		}
		s.closer.done()
	}()

	timer := time.NewTimer(time.Duration(defaultShutdownDelaySeconds) * time.Second)
	select {
	case <-timer.C:
		s.grpcServer.Stop()
	case <-s.closer.shutdownContext.Done():
		timer.Stop()
	}
	timer = nil
}

func (s *Srv[T]) registerServer(host string, port int) error {
	if s.registry == nil {
		wlog.Warn("start server without register")
		return nil
	}

	var (
		err             error
		registerAddress = host
	)

	if registerAddress == "::" {
		if registerAddress, err = utils.GetHostIP(); err != nil {
			return err
		}
	}

	metaData := register.NodeMeta{
		Host:    registerAddress,
		Port:    port,
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
	return nil
}
