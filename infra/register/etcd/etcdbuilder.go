package etcd

import (
	"context"
	"errors"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"

	"github.com/spacegrower/watermelon/infra/graceful"
	ide "github.com/spacegrower/watermelon/infra/internal/definition"
	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

var (
	liveTime int64 = 12
)

func SetKeepAlivePeriod(p int64) {
	if p < 1 {
		return
	}
	liveTime = p * 3
}

func init() {
	manager.RegisterKV(ide.ETCDPrefixKey{}, "/watermelon")
}

func GetETCDPrefixKey() string {
	return utils.PathJoin(manager.ResolveKV(ide.ETCDPrefixKey{}).(string), "service")
}

var (
	ErrTxnPutFailure = errors.New("txn execute failure")
)

// func generateServiceKey(orgid string, namespace, serviceName, nodeID string, port int) string {
// 	return fmt.Sprintf("%s/%s/%s/%s/node/%s:%d", GetETCDPrefixKey(), orgid, namespace, serviceName, nodeID, port)
// }

type Meta interface {
	RegisterKey() string
	Value() string
}

type kvstore[T Meta] struct {
	once       sync.Once
	ctx        context.Context
	cancelFunc context.CancelFunc
	client     *clientv3.Client
	metas      []T
	log        wlog.Logger
	leaseID    clientv3.LeaseID
}

func MustSetupEtcdRegister() register.ServiceRegister[NodeMeta] {
	client := manager.MustResolveEtcdClient()
	return NewEtcdRegister[NodeMeta](client)
}

func NewEtcdRegister[T Meta](client *clientv3.Client) register.ServiceRegister[T] {
	ctx, cancel := context.WithCancel(context.Background())
	return &kvstore[T]{
		ctx:        ctx,
		cancelFunc: cancel,
		client:     client,
		log:        wlog.With(zap.String("component", "etcd-register")),
		leaseID:    clientv3.NoLease,
	}
}

func (s *kvstore[T]) Append(meta T) error {
	for _, v := range s.metas {
		if v.RegisterKey() == meta.RegisterKey() {
			return nil
		}
	}
	s.metas = append(s.metas, meta)
	return nil
}

func (s *kvstore[T]) Register() error {
	s.log.Debug("start register")
	var (
		err error
	)

	if err = s.register(); err != nil {
		s.log.Error("failed to register server", zap.Error(err))
		return err
	}

	if err = s.keepAlive(s.leaseID); err != nil {
		s.log.Error("failed to keep alive register lease", zap.Error(err),
			zap.Int64("lease_id", int64(s.leaseID)))
		return err
	}

	s.once.Do(func() {
		graceful.RegisterPreShutDownHandlers(func() {
			s.DeRegister()
		})
	})

	return nil
}

func (s *kvstore[T]) register() error {
	ctx, cancel := context.WithTimeout(s.ctx, time.Second*3)
	defer cancel()
	if s.leaseID == clientv3.NoLease {
		resp, err := s.client.Grant(ctx, liveTime)
		if err != nil {
			return err
		}

		s.leaseID = resp.ID
	}

	for _, v := range s.metas {
		registerKey := utils.PathJoin(GetETCDPrefixKey(), v.RegisterKey())
		value := v.Value()

		if err := s.putkey(registerKey, value); err != nil {
			s.log.Error("failed to put register key", zap.Error(err), zap.String("key", registerKey))
			return err
		}
		s.log.Info("service registered successful",
			zap.String("key", registerKey),
			zap.String("meta", value))
	}

	return nil
}

func (s *kvstore[T]) init() {
	if s.leaseID != clientv3.NoLease {
		if err := s.revoke(s.leaseID); err != nil {
			s.log.Error("failed to revoke lease", zap.Error(err))
		}
	}
}

func (s *kvstore[T]) putkey(k, v string) error {
	if s.leaseID == clientv3.NoLease {
		return errors.New("no lease")
	}
	ctx, cancel := context.WithTimeout(s.ctx, time.Second*3)
	defer cancel()
	if _, err := s.client.Put(ctx, k, v, clientv3.WithLease(s.leaseID)); err != nil {
		return err
	}
	return nil
}

func (s *kvstore[T]) DeRegister() error {
	if s.ctx.Err() != nil {
		s.log.Warn("ctx is already cancelled", zap.Error(s.ctx.Err()))
		return nil
	}
	s.log.Warn("called deregister", zap.Any("service", s.metas))
	defer s.cancelFunc()

	s.init()
	return nil
}

func (s *kvstore[T]) Close() {
	// just close kvstore not etcd client
	s.DeRegister()
}

func (s *kvstore[T]) keepAlive(leaseID clientv3.LeaseID) error {
	ch, err := s.client.KeepAlive(s.ctx, leaseID)
	if err != nil {
		return err
	}

	go safe.Run(func() {
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					s.log.Debug("failed to keepalive lease", zap.Any("service", s.metas), zap.Any("context", s.ctx.Err()), zap.Int64("lease_id", int64(leaseID)))
					select {
					case <-s.ctx.Done():
						s.Close()
						return
					default:
					}

					s.reRegister()
					return
				}
			case <-s.ctx.Done():
				s.log.Warn("etcd-register is down, context cancelled", zap.Any("service", s.metas), zap.Error(s.ctx.Err()))
				s.Close()
				return
			}
		}
	})

	return nil
}

func (s *kvstore[T]) reRegister() {
	s.init()
	for {
		select {
		case <-s.ctx.Done():
			s.log.Warn("stop to register, context cancelled", zap.Error(s.ctx.Err()), zap.Any("service", s.metas))
		default:
			if err := s.Register(); err != nil {
				time.Sleep(time.Second)
				continue
			}
		}

		return
	}
}

func (s *kvstore[T]) revoke(leaseID clientv3.LeaseID) error {
	s.log.Debug("revoke lease", zap.Int64("lease", int64(leaseID)))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	if _, err := s.client.Revoke(ctx, leaseID); err != nil {
		return err
	}
	s.leaseID = clientv3.NoLease

	return nil
}
