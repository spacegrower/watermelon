package etcd_test

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/spacegrower/watermelon/infra"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/register/etcd"

	clientv3 "go.etcd.io/etcd/client/v3"
)

func TestEtcdReRegister(t *testing.T) {
	infra.RegisterETCDRegisterPrefixKey("")
	cfg := clientv3.Config{
		Endpoints: []string{""}, // cluster list
		Username:  "",
		Password:  "",
	}

	if len(cfg.Endpoints) == 0 {
		return
	}

	infra.RegisterEtcdClient(cfg)

	r := etcd.MustSetupEtcdRegister()

	metas := []etcd.NodeMeta{
		{
			Region:       "rrrr",
			OrgID:        "1",
			Namespace:    "ggg",
			NodeWeight:   1,
			RegisterTime: time.Now().UnixNano(),
			NodeMeta: register.NodeMeta{
				Host:        "localhost",
				Port:        111,
				ServiceName: "localtest",
				Runtime:     runtime.Version(),
			},
		},
	}

	r.SetMetas(metas)

	fmt.Println("---- start register ----")

	if err := r.Register(); err != nil {
		t.Fatal(err)
	}

	fmt.Println("---- start change node weight ----")
	metas[0].NodeWeight = 2

	r.SetMetas(metas)

	if err := r.Register(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Second * 10)
}
