package levelweight

import (
	"math"
	"testing"
)

func TestLevelWeight(t *testing.T) {
	SetThreshold(3)
	var weight = []int32{1, 2, 3, 4, 20, 20, 40, 50, 100, 300}

	var totalWeight int32
	for _, v := range weight {
		totalWeight += v
	}

	levelLine := totalWeight / int32(len(weight))
	t.Log("level line", levelLine)

	var (
		topLevel []int32
		others   []int32
	)
	for _, v := range weight {
		if v >= levelLine {
			topLevel = append(topLevel, v)
		} else {
			others = append(others, v)
		}
	}

	yardstick := threshold
	if yardstick == 0 {
		yardstick = int(math.Ceil(float64(len(weight)) / 2))
	}

	t.Log(yardstick)

	if len(topLevel) >= yardstick {
		t.Log("used top level nodes", len(topLevel))
	}
}
