package etcd

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/graceful"
	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

const (
	liveTime        int64 = 5
	ETCD_KEY_PREFIX       = "/watermelon/service"
)

func generateServiceKey(namespace, serviceName, nodeID string, port int) string {
	return fmt.Sprintf("%s/%s/%s/node/%s:%d", ETCD_KEY_PREFIX, namespace, serviceName, nodeID, port)
}

type kvstore struct {
	once       sync.Once
	ctx        context.Context
	cancelFunc context.CancelFunc
	client     *clientv3.Client
	meta       register.NodeMeta
	log        wlog.Logger
	leaseID    clientv3.LeaseID
}

func MustSetupEtcdRegister() register.ServiceRegister {
	client := manager.MustResolveEtcdClient()
	ctx, cancel := context.WithCancel(client.Ctx())
	return &kvstore{
		ctx:        ctx,
		cancelFunc: cancel,
		client:     client,
		log:        wlog.With(zap.String("component", "etcd-register")),
	}
}

func NewEtcdRegister(client *clientv3.Client) register.ServiceRegister {
	ctx, cancel := context.WithCancel(client.Ctx())
	return &kvstore{
		ctx:        ctx,
		cancelFunc: cancel,
		client:     client,
		log:        wlog.With(zap.String("component", "etcd-register")),
	}
}

func (s *kvstore) Init(meta register.NodeMeta) error {
	// customize your register logic
	meta.Weight = utils.GetEnvWithDefault(definition.NodeWeightENVKey, 100, func(val string) (int, error) {
		res, err := strconv.Atoi(val)
		if err != nil {
			return 0, err
		}
		return res, nil
	})

	s.meta = meta
	return nil
}

func (s *kvstore) Register() error {
	s.log.Debug("start register")
	var err error
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
		graceful.RegisterShutDownHandlers(func() {
			s.client.Close()
		})
	})

	return nil
}

func (s *kvstore) register() error {
	ctx, cancel := context.WithTimeout(s.ctx, time.Second*3)
	defer cancel()
	if s.leaseID == clientv3.NoLease {
		resp, err := s.client.Grant(ctx, liveTime)
		if err != nil {
			return err
		}

		s.leaseID = resp.ID
	}
	registerKey := generateServiceKey(s.meta.Namespace, s.meta.ServiceName, s.meta.Host, s.meta.Port)
	if _, err := s.client.Put(ctx, registerKey, s.meta.ToJson(), clientv3.WithLease(s.leaseID)); err != nil {
		return err
	}

	s.log.Info("service registered successful",
		zap.String("namespace", s.meta.Namespace),
		zap.String("name", s.meta.ServiceName),
		zap.String("address", fmt.Sprintf("%s:%d", s.meta.Host, s.meta.Port)))

	return nil
}

func (s *kvstore) DeRegister() error {
	if s.leaseID != clientv3.NoLease {
		return s.revoke(s.leaseID)
	}
	return nil
}

func (s *kvstore) Close() {
	// just close kvstore not etcd client
	s.DeRegister()
	s.cancelFunc()
}

func (s *kvstore) keepAlive(leaseID clientv3.LeaseID) error {
	ch, err := s.client.KeepAlive(s.ctx, leaseID)
	if err != nil {
		return err
	}

	go safe.Run(func() {
		for {
			select {
			case _, ok := <-ch:
				if !ok {
					s.revoke(leaseID)
					s.reRegister()
					return
				}
			case <-s.ctx.Done():
				s.log.Info("context done")
				return
			}
		}
	})

	return nil
}

func (s *kvstore) reRegister() {
	s.leaseID = clientv3.NoLease
	for {
		select {
		case <-s.ctx.Done():
		default:
			if err := s.Register(); err != nil {
				time.Sleep(time.Second)
				continue
			}
		}

		return
	}
}

func (s *kvstore) revoke(leaseID clientv3.LeaseID) error {
	s.log.Debug("revoke lease", zap.Int64("lease", int64(leaseID)))
	ctx, cancel := context.WithTimeout(s.ctx, time.Second*3)
	defer cancel()
	if _, err := s.client.Revoke(ctx, leaseID); err != nil {
		return err
	}
	s.leaseID = clientv3.NoLease

	return nil
}
