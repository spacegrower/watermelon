package etcd

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/avast/retry-go/v4"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc/connectivity"

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

type Meta interface {
	RegisterKey() string
	Value() string
}

type kvstore[T Meta] struct {
	ctx         context.Context
	cancelFunc  context.CancelFunc
	client      *clientv3.Client
	metas       []T
	log         wlog.Logger
	leaseID     clientv3.LeaseID
	changedChan chan struct{}
	locker      *sync.Mutex
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
		locker:     &sync.Mutex{},
	}
}

func (s *kvstore[T]) SetMetas(metas []T) {
	s.locker.Lock()
	s.metas = metas
	s.locker.Unlock()
}

func (s *kvstore[T]) Register() error {
	s.log.Debug("start register")
	s.deRegister()

	s.locker.Lock()
	defer s.locker.Unlock()

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

	return nil
}

func (s *kvstore[T]) register() error {
	ctx, cancel := context.WithTimeout(s.ctx, time.Second*3)
	defer cancel()
	if s.client.ActiveConnection().GetState() == connectivity.Shutdown {
		s.cancelFunc()
		return errors.New("etcd client is shutdown")
	}
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

		if err := retry.Do(
			func() error {
				return s.putkey(registerKey, value)
			},
			retry.Attempts(3),
			retry.Delay(time.Second),
			retry.OnRetry(func(n uint, err error) {
				s.log.Warn("retrying registration", zap.Uint("attempt", n+1), zap.Error(err), zap.String("key", registerKey))
			}),
			retry.LastErrorOnly(true),
		); err != nil {
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
	if s.changedChan != nil {
		close(s.changedChan)
	}
	s.changedChan = make(chan struct{})
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

func (s *kvstore[T]) deRegister() error {
	s.locker.Lock()
	defer s.locker.Unlock()
	s.init()
	return nil
}

func (s *kvstore[T]) Close() {
	// just close kvstore not etcd client
	s.deRegister()

	if s.ctx.Err() != nil {
		s.log.Warn("ctx is already cancelled", zap.Error(s.ctx.Err()))
		return
	}
	s.log.Warn("called deregister", zap.Any("service", s.metas))
	defer s.cancelFunc()
}

func (s *kvstore[T]) keepAlive(leaseID clientv3.LeaseID) error {
	// 检查连接状态
	if err := s.checkConnectionHealth(); err != nil {
		return fmt.Errorf("connection health check failed: %w", err)
	}

	ch, err := s.client.KeepAlive(s.ctx, leaseID)
	if err != nil {
		return err
	}
	changed := s.changedChan

	go safe.Run(func() {
		for {
			select {
			case <-changed:
				return
			case _, ok := <-ch:
				if changed != s.changedChan {
					return
				}
				if !ok {
					s.log.Warn("failed to keepalive lease, channel closed", 
						zap.Any("service", s.metas), 
						zap.Any("context", s.ctx.Err()), 
						zap.Int64("lease_id", int64(leaseID)))
					
					// 检查连接状态
					if connErr := s.checkConnectionHealth(); connErr != nil {
						s.log.Error("etcd connection lost, attempting to reconnect", zap.Error(connErr))
					}
					
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
				return
			}
		}
	})

	return nil
}

func (s *kvstore[T]) reRegister() {
	if len(s.metas) == 0 {
		return
	}

	backoff := time.Second
	maxBackoff := time.Second * 30
	
	for {
		select {
		case <-s.ctx.Done():
			s.log.Warn("stop to register, context cancelled", zap.Error(s.ctx.Err()), zap.Any("service", s.metas))
			return
		default:
			// 检查连接健康状态
			if connErr := s.checkConnectionHealth(); connErr != nil {
				s.log.Warn("etcd connection unhealthy, waiting for recovery", 
					zap.Error(connErr), 
					zap.Duration("backoff", backoff))
			} else {
				// 连接正常，尝试重新注册
				if err := s.Register(); err != nil {
					s.log.Error("failed to re-register service", 
						zap.Error(err), 
						zap.Duration("backoff", backoff),
						zap.Any("service", s.metas))
				} else {
					s.log.Info("successfully re-registered service", zap.Any("service", s.metas))
					return
				}
			}
			
			// 指数退避
			time.Sleep(backoff)
			if backoff < maxBackoff {
				backoff *= 2
			}
		}
	}
}

func (s *kvstore[T]) checkConnectionHealth() error {
	if s.client == nil {
		return errors.New("etcd client is nil")
	}
	
	state := s.client.ActiveConnection().GetState()
	if state == connectivity.Shutdown {
		return errors.New("etcd connection is shutdown")
	}
	
	// 额外检查etcd健康状态
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	
	_, err := s.client.Get(ctx, "health-check")
	return err
}

func (s *kvstore[T]) revoke(leaseID clientv3.LeaseID) error {
	s.log.Debug("revoke lease", zap.Int64("lease", int64(leaseID)))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer func() {
		cancel()
		s.leaseID = clientv3.NoLease
	}()
	if _, err := s.client.Revoke(ctx, leaseID); err != nil {
		return err
	}

	return nil
}
