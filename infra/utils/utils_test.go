package utils

import (
	"testing"
)

func Test_PointerValue(t *testing.T) {
	result := PointerValue(1)
	t.Log(result)
}

func Test_PathJoin(t *testing.T) {
	res := PathJoin("watermelon/a", "service")
	t.Log(res)
}
