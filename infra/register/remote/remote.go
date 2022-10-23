package remote

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/etc/remote/spacegrower/remote"
	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/graceful"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

type remoteRegistry struct {
	once       sync.Once
	ctx        context.Context
	cancelFunc context.CancelFunc
	client     remote.Registry_RegisterClient
	meta       register.NodeMeta
	log        wlog.Logger
}

func NewRemoteRegister(endpoint string, opts ...grpc.DialOption) (register.ServiceRegister, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cc, err := grpc.DialContext(ctx, endpoint, opts...)
	if err != nil {
		cancel()
		return nil, err
	}

	rr := &remoteRegistry{
		ctx:        ctx,
		cancelFunc: cancel,
		log:        wlog.With(zap.String("component", "remote-register")),
	}

	if rr.client, err = remote.NewRegistryClient(cc).Register(ctx); err != nil {
		return nil, err
	}

	go safe.Run(func() {
		for {
			select {
			case <-rr.ctx.Done():
				rr.client.CloseSend()
				return
			default:
				resp, err := rr.client.Recv()
				if err != nil {
					rr.reRegister()
					return
				}

				rr.log.Debug("TODO", zap.String("command", resp.Command), zap.Any("args", resp.Args))
			}
		}
	})

	return rr, nil
}

func (s *remoteRegistry) Init(meta register.NodeMeta) error {
	// customize your register logic
	meta.Weight = utils.GetEnvWithDefault(definition.NodeWeightENVKey, 100, func(val string) (int32, error) {
		res, err := strconv.Atoi(val)
		if err != nil {
			return 0, err
		}
		return int32(res), nil
	})

	s.meta = meta
	return nil
}

func (s *remoteRegistry) Register() error {
	s.log.Debug("start register")

	var err error
	if err = s.register(); err != nil {
		s.log.Error("failed to register server", zap.Error(err))
		return err
	}

	s.once.Do(func() {
		graceful.RegisterShutDownHandlers(func() {
			s.Close()
		})
	})

	return nil
}

func (s *remoteRegistry) register() error {
	meta := &remote.ServiceInfo{
		Region:      s.meta.Region,
		Namespace:   s.meta.Namespace,
		ServiceName: s.meta.ServiceName,
		Host:        s.meta.Host,
		Port:        int32(s.meta.Port),
		Weight:      s.meta.Weight,
		Runtime:     s.meta.Runtime,
		Tags:        s.meta.Tags,
	}

	for k, v := range meta.Methods {
		meta.Methods[k] = v
	}

	if err := s.client.Send(meta); err != nil {
		return err
	}

	s.log.Info("service registered successful",
		zap.String("namespace", s.meta.Namespace),
		zap.String("name", s.meta.ServiceName),
		zap.String("address", fmt.Sprintf("%s:%d", s.meta.Host, s.meta.Port)))

	return nil
}

func (s *remoteRegistry) DeRegister() error {
	s.cancelFunc()
	return nil
}

func (s *remoteRegistry) Close() {
	// just close kvstore not etcd client
	s.DeRegister()
}

func (s *remoteRegistry) reRegister() {
	for {
		select {
		case <-s.ctx.Done():
			s.client.CloseSend()
		default:
			if err := s.Register(); err != nil {
				time.Sleep(time.Second)
				continue
			}
		}

		return
	}
}
