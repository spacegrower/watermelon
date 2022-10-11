package register

import (
	"encoding/json"

	"google.golang.org/grpc"
)

type ServiceRegister interface {
	Register() error
	Init(server *grpc.Server, region, namespace, host string, port int, tags map[string]string) error
}

type NodeMetaKey struct{}

type NodeMeta struct {
	Region       string            `json:"region"`
	Namespace    string            `json:"namespace"`
	ServiceName  string            `json:"service_name"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	Weight       int               `json:"weight"`
	Tags         map[string]string `json:"tags"`
	Methods      []string          `json:"methods"`
	Runtime      string            `json:"runtime"`
	Version      string            `json:"version"`
	RegisterTime int64             `json:"register_time"`
}

func (n NodeMeta) ToJson() string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
