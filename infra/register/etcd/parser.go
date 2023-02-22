package etcd

import (
	"encoding/json"
	"fmt"

	"github.com/spacegrower/watermelon/infra/register"
)

type NodeMeta struct {
	Region       string            `json:"region"`
	OrgID        string            `json:"org_id"`
	Namespace    string            `json:"namespace"`
	Weight       int32             `json:"weight"`
	Tags         map[string]string `json:"tags"`
	RegisterTime int64             `json:"register_time"`
	register.NodeMeta
}

func (n NodeMeta) WithMeta(meta register.NodeMeta) NodeMeta {
	n.NodeMeta = meta
	return n
}

func (n NodeMeta) Value() string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func (n NodeMeta) RegisterKey() string {
	return fmt.Sprintf("%s/%s/%s/node/%s:%d", n.OrgID, n.Namespace, n.ServiceName, n.Host, n.Port)
}

func (n NodeMeta) Weigth() int32 {
	return n.Weight
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
