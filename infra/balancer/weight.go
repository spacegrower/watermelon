package balancer

import (
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

func init() {
	balancer.Register(base.NewBalancerBuilder(WeightRobinName,
		new(weightRobinBalancer),
		base.Config{HealthCheck: true}))
}

const WeightRobinName = "watermelon_weight_round_robin"

type weightRobinBalancer struct {
}

type weightNodeStore struct {
	Method      string
	Nodes       []*weigthNode
	Count       int
	TotalWeight int
}

type weigthNode struct {
	Node
	Weight        int
	CurrentWeight int
}

type weightRobinPicker struct {
	store map[string]*weightNodeStore
}

func (b *weightRobinBalancer) Build(info base.PickerBuildInfo) balancer.Picker {
	p := &weightRobinPicker{
		store: make(map[string]*weightNodeStore),
	}

	for subConn, subConnInfo := range info.ReadySCs {
		attr := subConnInfo.Address.BalancerAttributes.Value(register.NodeMetaKey{})
		if attr == nil {
			continue
		}

		meta, ok := attr.(register.NodeMeta)
		if !ok {
			wlog.Error("failed to parse node balancer attributes to register.NodeMeta")
			continue
		}

		for _, v := range meta.Methods {
			fullMethodName := utils.PathJoin(meta.ServiceName, v.Name)
			if _, exist := p.store[fullMethodName]; !exist {
				p.store[fullMethodName] = &weightNodeStore{
					Method: v.Name,
				}
			}
			p.store[fullMethodName].Count += 1
			p.store[fullMethodName].TotalWeight += meta.Weight
			p.store[fullMethodName].Nodes = append(p.store[fullMethodName].Nodes, &weigthNode{
				Node: Node{
					Address: subConnInfo.Address,
					SubConn: subConn,
				},
				Weight:        meta.Weight,
				CurrentWeight: meta.Weight,
			})
		}
	}
	return p
}

func (p *weightRobinPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	list, ok := p.store[info.FullMethodName]

	if !ok || list.Count == 0 {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}

	var (
		maxWeight     int
		selectedIndex int
	)
	for i, n := range list.Nodes {
		n.CurrentWeight += n.Weight
		if n.CurrentWeight > maxWeight {
			maxWeight = n.CurrentWeight
			selectedIndex = i
		}
	}

	list.Nodes[selectedIndex].CurrentWeight -= list.Nodes[selectedIndex].Weight

	return balancer.PickResult{
		SubConn: list.Nodes[selectedIndex].SubConn,
		Done:    nil,
	}, nil
}
