package register

type ServiceRegister[T any] interface {
	Register() error
	DeRegister() error
	Close()
	Append(T) error
}

type NodeMetaKey struct{}

type NodeMeta struct {
	Host        string           `json:"host"`
	Port        int              `json:"port"`
	ServiceName string           `json:"service_name"`
	GrpcMethods []GrpcMethodInfo `json:"methods"`
	Runtime     string           `json:"runtime"`
	Version     string           `json:"version"`
}

type GrpcMethodInfo struct {
	// Name is the method name only, without the service name or package name.
	Name string `json:"name"`
	// IsClientStream indicates whether the RPC is a client streaming RPC.
	IsClientStream bool `json:"is_client_stream"`
	// IsServerStream indicates whether the RPC is a server streaming RPC.
	IsServerStream bool `json:"is_server_stream"`
}

// func (n NodeMeta) ToJson() string {
// 	raw, _ := json.Marshal(n)
// 	return string(raw)
// }

// func (n NodeMeta) RegisterKey() string {
// 	return fmt.Sprintf("%s/%s/%s/node/%s:%d", n.OrgID, n.Namespace, n.ServiceName, n.Host, n.Port)
// }
