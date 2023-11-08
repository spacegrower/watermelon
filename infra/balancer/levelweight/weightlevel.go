package levelweight

import (
	"encoding/json"
	"math"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/balancer/base"

	wbalancer "github.com/spacegrower/watermelon/infra/balancer"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
	"github.com/spacegrower/watermelon/infra/wlog"
)

const Name = "watermelon_level_weight_round"

var threshold int

func SetThreshold(i int) {
	threshold = i
}

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
	wbalancer.Node
	Weight        int32
	CurrentWeight int32
}

type CacheNodes[T interface {
	Weigth() int32
	Service() string
	Methods() []string
}] struct {
	wbalancer.Node
	meta T
}

type weightRobinPicker struct {
	store map[string]*weightNodeStore
}

func (b *WeightRobinBalancer[T]) Build(info base.PickerBuildInfo) balancer.Picker {
	p := &weightRobinPicker{
		store: make(map[string]*weightNodeStore),
	}

	var (
		totalWeight int32
		cached      []CacheNodes[T]
	)
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

		totalWeight += meta.Weigth()
		cached = append(cached, CacheNodes[T]{
			meta: meta,
			Node: wbalancer.Node{
				Address: subConnInfo.Address,
				SubConn: subConn,
			},
		})
	}

	levelLine := totalWeight / int32(len(info.ReadySCs))
	var (
		topLevel []CacheNodes[T]
	)

	for _, v := range cached {
		if v.meta.Weigth() >= levelLine {
			topLevel = append(topLevel, v)
		}
	}

	yardstick := threshold
	if yardstick == 0 {
		yardstick = int(math.Ceil(float64(len(cached)) / 2))
	}

	var joinToBalanceNodes []CacheNodes[T]
	if len(topLevel) >= yardstick {
		joinToBalanceNodes = topLevel
	} else {
		joinToBalanceNodes = cached
	}

	for _, node := range joinToBalanceNodes {
		for _, v := range node.meta.Methods() {
			fullMethodName := utils.PathJoin(node.meta.Service(), v)
			if _, exist := p.store[fullMethodName]; !exist {
				p.store[fullMethodName] = &weightNodeStore{
					Method: v,
				}
			}
			p.store[fullMethodName].Count += 1
			p.store[fullMethodName].TotalWeight += node.meta.Weigth()
			p.store[fullMethodName].Nodes = append(p.store[fullMethodName].Nodes, &weigthNode{
				Node: wbalancer.Node{
					Address: node.Address,
					SubConn: node.SubConn,
				},
				Weight:        node.meta.Weigth(),
				CurrentWeight: node.meta.Weigth(),
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
