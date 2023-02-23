package balancer

import (
	"encoding/json"

	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"
)

const WeightRobinName = "watermelon_weight_round_robin"

type WeightRobinBalancer[T interface {
	Weigth() int32
	Service() string
	Methods() []string
}] struct {
}

func (w *WeightRobinBalancer[T]) Parser(meta string) T {
	var a T
	json.Unmarshal([]byte(meta), &a)
	return a
}

type weightNodeStore struct {
	Method      string
	Nodes       []*weigthNode
	Count       int
	TotalWeight int32
}

type weigthNode struct {
	Node
	Weight        int32
	CurrentWeight int32
}

type weightRobinPicker struct {
	store map[string]*weightNodeStore
}

func (b *WeightRobinBalancer[T]) Build(info base.PickerBuildInfo) balancer.Picker {
	p := &weightRobinPicker{
		store: make(map[string]*weightNodeStore),
	}

	for subConn, subConnInfo := range info.ReadySCs {
		attr := subConnInfo.Address.BalancerAttributes.Value(register.NodeMetaKey{})
		if attr == nil {
			continue
		}

		meta, ok := attr.(T)
		if !ok {
			wlog.Error("failed to parse node balancer attributes to T")
			continue
		}

		for _, v := range meta.Methods() {
			fullMethodName := utils.PathJoin(meta.Service(), v)
			if _, exist := p.store[fullMethodName]; !exist {
				p.store[fullMethodName] = &weightNodeStore{
					Method: v,
				}
			}
			p.store[fullMethodName].Count += 1
			p.store[fullMethodName].TotalWeight += meta.Weigth()
			p.store[fullMethodName].Nodes = append(p.store[fullMethodName].Nodes, &weigthNode{
				Node: Node{
					Address: subConnInfo.Address,
					SubConn: subConn,
				},
				Weight:        meta.Weigth(),
				CurrentWeight: meta.Weigth(),
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
		maxWeight     int32
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
