package balancer

import (
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/resolver"
)

type Node struct {
	Address resolver.Address
	SubConn balancer.SubConn
}
