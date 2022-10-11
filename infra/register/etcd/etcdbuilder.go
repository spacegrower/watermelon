package etcd

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/graceful"
	"github.com/spacegrower/watermelon/infra/internal/manager"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/version"
	"github.com/spacegrower/watermelon/infra/wlog"
	"github.com/spacegrower/watermelon/pkg/safe"
)

const (
	liveTime        int64 = 10
	ETCD_KEY_PREFIX       = "/watermelon/service"
)

func generateServiceKey(namespace, serviceName, nodeID string, port int) string {
	return fmt.Sprintf("%s/%s/%s/node/%s:%d", ETCD_KEY_PREFIX, namespace, serviceName, nodeID, port)
}

type discover struct{}

type kvstore struct {
	once       sync.Once
	ctx        context.Context
	cancelFunc context.CancelFunc
	client     *clientv3.Client
	meta       *register.NodeMeta
	log        wlog.Logger
}

func MustSetupEtcdRegister() register.ServiceRegister {
	ctx, cancel := context.WithCancel(context.Background())
	return &kvstore{
		ctx:        ctx,
		cancelFunc: cancel,
		client:     manager.MustResolveEtcdClient(),
		log:        wlog.With(zap.String("component", "etcd-register")),
	}
}

func (s *kvstore) Init(server *grpc.Server, region, namespace, host string, port int, tags map[string]string) error {
	meta := &register.NodeMeta{
		Region:       region,
		Namespace:    namespace,
		Host:         host,
		Port:         port,
		Weight:       100,
		Runtime:      runtime.Version(),
		Version:      version.V,
		RegisterTime: time.Now().Unix(),
	}

	meta.Weight = utils.GetEnvWithDefault(definition.NodeWeightENVKey, 100, func(val string) (int, error) {
		res, err := strconv.Atoi(val)
		if err != nil {
			return 0, err
		}
		return res, nil
	})

	for name, methods := range server.GetServiceInfo() {
		meta.ServiceName = name
		for _, v := range methods.Methods {
			meta.Methods = append(meta.Methods, v.Name)
		}
	}

	s.meta = meta
	return nil
}

func (s *kvstore) Register() error {
	leaseID, err := s.register()
	if err != nil {
		return err
	}

	if err = s.keepAlive(leaseID); err != nil {
		return err
	}

	s.once.Do(func() {
		graceful.RegisterShutDownHandlers(func() {
			s.deRegister()
		})
	})

	return nil
}

func (s *kvstore) register() (clientv3.LeaseID, error) {
	resp, err := s.client.Grant(s.client.Ctx(), liveTime)
	if err != nil {
		return clientv3.NoLease, err
	}

	lease := resp.ID
	registerKey := generateServiceKey(s.meta.Namespace, s.meta.ServiceName, s.meta.Host, s.meta.Port)
	_, err = s.client.Put(s.client.Ctx(), registerKey, s.meta.ToJson(), clientv3.WithLease(lease))

	s.log.Info("service registered successful",
		zap.String("namespace", s.meta.Namespace),
		zap.String("name", s.meta.ServiceName),
		zap.String("address", fmt.Sprintf("%s:%d", s.meta.Host, s.meta.Port)))

	return lease, err
}

func (s *kvstore) deRegister() error {
	s.cancelFunc()
	return s.client.Close()
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

					time.Sleep(time.Millisecond * 100)

					if err := s.Register(); err != nil {
						wlog.Error("failed to re-register", zap.Error(err))
					}
					return
				}
			case <-s.ctx.Done():
				s.revoke(leaseID)
				return
			}
		}
	})

	return nil
}

func (s *kvstore) revoke(leaseID clientv3.LeaseID) error {
	if _, err := s.client.Revoke(s.client.Ctx(), leaseID); err != nil {
		return err
	}

	return nil
}
