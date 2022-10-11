package srv

import (
	"context"
	"os"
	"os/signal"

	"github.com/spacegrower/watermelon/infra/wlog"
	"go.uber.org/zap"
)

type Srv interface {
	RunUntil(signals ...os.Signal) error
}

type Injection interface {
	Start() error
	Stop() error
}

type srvImpl struct {
	Injection
}

func NewSrv(srv Injection) Srv {
	return &srvImpl{
		srv,
	}
}

func (s *srvImpl) RunUntil(signals ...os.Signal) error {
	ctx, cancel := context.WithCancel(newContextWithSignal(signals...))
	go func() {
		defer cancel()
		if err := s.Start(); err != nil {
			wlog.Error("failed to start srv", zap.Error(err))
		}
	}()
	return s.StopUntil(ctx)
}

func (s *srvImpl) StopUntil(ctx context.Context) error {
	<-ctx.Done()
	return s.Stop()
}

func newContextWithSignal(signals ...os.Signal) context.Context {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	ctx, cancel := context.WithCancel(context.TODO())

	go func() {
		<-ch
		cancel()
	}()
	return ctx
}
