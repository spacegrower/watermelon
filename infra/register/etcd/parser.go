package etcd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
)

type NodeMeta struct {
	Region       string            `json:"region"`
	OrgID        string            `json:"org_id"`
	Namespace    string            `json:"namespace"`
	NodeWeight   int32             `json:"weight"`
	Tags         map[string]string `json:"tags"`
	RegisterTime int64             `json:"register_time"`
	register.NodeMeta
}

func (n NodeMeta) WithMeta(meta register.NodeMeta) NodeMeta {
	n.NodeMeta = meta
	return n
}

func (n NodeMeta) Equal(o any) bool {
	ov, ok := o.(NodeMeta)
	if !ok {
		return false
	}

	return n.ServiceName == ov.ServiceName &&
		n.Host == ov.Host &&
		n.Port == ov.Port &&
		n.OrgID == ov.OrgID &&
		n.Region == ov.Region &&
		n.Namespace == ov.Namespace &&
		n.NodeWeight == ov.NodeWeight &&
		n.RegisterTime == ov.RegisterTime
}

func (n NodeMeta) Value() string {
	// customize your register value logic
	n.NodeWeight = utils.GetEnvWithDefault(definition.NodeWeightENVKey, n.NodeWeight, func(val string) (int32, error) {
		res, err := strconv.Atoi(val)
		if err != nil {
			return 0, err
		}
		return int32(res), nil
	})

	n.RegisterTime = time.Now().Unix()

	raw, _ := json.Marshal(n)
	return string(raw)
}

func (n NodeMeta) RegisterKey() string {
	return fmt.Sprintf("%s/%s/%s/node/%s:%d", n.OrgID, n.Namespace, n.ServiceName, n.Host, n.Port)
}

func (n NodeMeta) Weight() int32 {
	return n.NodeWeight
}

func (n NodeMeta) Service() string {
	return n.ServiceName
}

func (n NodeMeta) Methods() []string {
	var methods []string
	for _, v := range n.GrpcMethods {
		methods = append(methods, v.Name)
	}
	return methods
}
