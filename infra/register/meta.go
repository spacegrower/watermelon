package register

import (
	"encoding/json"

	"google.golang.org/grpc"
)

type ServiceRegister interface {
	Register() error
	Init(serviceName string, methods []grpc.MethodInfo, region, namespace, host string, port int, tags map[string]string) error
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
	Methods      []GrpcMethodInfo  `json:"methods"`
	Runtime      string            `json:"runtime"`
	Version      string            `json:"version"`
	RegisterTime int64             `json:"register_time"`
}

type GrpcMethodInfo struct {
	// Name is the method name only, without the service name or package name.
	Name string `json:"name"`
	// IsClientStream indicates whether the RPC is a client streaming RPC.
	IsClientStream bool `json:"is_client_stream"`
	// IsServerStream indicates whether the RPC is a server streaming RPC.
	IsServerStream bool `json:"is_server_stream"`
}

func (n NodeMeta) ToJson() string {
	raw, _ := json.Marshal(n)
	return string(raw)
}
