package firemelon

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/register"
	"github.com/spacegrower/watermelon/infra/utils"
)

// register node meta
type NodeMeta struct {
	OrgID        string
	System       string
	Region       string
	Weight       int32
	RegisterTime int64
	register.NodeMeta
}

func (n NodeMeta) Equal(b any) bool {
	cb, ok := b.(NodeMeta)
	if !ok {
		return false
	}
	return n.OrgID == cb.OrgID &&
		n.Host == cb.Host &&
		n.Port == cb.Port &&
		n.Region == cb.Region &&
		n.System == cb.System
}

func (n NodeMeta) WithMeta(meta register.NodeMeta) NodeMeta {
	n.NodeMeta = meta
	return n
}

func (n NodeMeta) Value() string {
	// customize your register value logic
	n.Weight = utils.GetEnvWithDefault(definition.NodeWeightENVKey, n.Weight, func(val string) (int32, error) {
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
	return fmt.Sprintf("%s/%s/%s/node/%s:%d", n.OrgID, n.System, n.ServiceName, n.Host, n.Port)
}
