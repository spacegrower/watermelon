<p align="center">
    <img src="docs/images/logo.png" alt="banner" width="180px">
</p>

<p align="center">
    Watermelon, A gRPC-service base tools
</p>

#### An unprecedented way of organizing middleware

```go
service := &GreeterSrv{} // greeter srvice impl
srvBuilder := watermelon.NewServer()
srv := srvBuilder(func(srv *grpc.Server) {
    greeter.RegisterGreeterServer(srv, service)
})

a := srv.Group()
a.Use(func(ctx context.Context) error {
    if err := middleware.Next(ctx); err != nil {
        fmt.Println("The output can only be obtained when accessing the SayHelloAgain method", err)
        return err
    }
    fmt.Println("The output can only be obtained when accessing the SayHello method")
    return nil
})

a.Handler(service.SayHello)

b := a.Group()
b.Use(func(ctx context.Context) error {
    fullMethod := middleware.GetFullMethodFrom(ctx)
    if filepath.Base(fullMethod) == "SayHelloAgain" {
        return status.Error(codes.Aborted, "Don't say good things a second time")
    }
    return nil
})
b.Handler(service.SayHelloAgain)

srv.RunUntil(syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
```

### Customizable

Built entirely on generics, making it easier to extend.  
Customizable service registration and discovery component.

See example/customize for more details.

```go
// Server
type Server[T interface {
	WithMeta(register.NodeMeta) T
}] func(register func(srv *grpc.Server), opts ...infra.Option[T]) *infra.Srv[T]

// Client
type ClientConn[T infra.ClientServiceNameGenerator] func(serviceName string, opts ...infra.ClientOptions[T]) (*grpc.ClientConn, error)

// Register
func NewEtcdRegister[T Meta](client *clientv3.Client) register.ServiceRegister[T]

// Resolver
func NewEtcdResolver[T any](client *clientv3.Client, af AllowFuncType[T]) wresolver.Resolver

```

