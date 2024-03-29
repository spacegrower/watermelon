package middleware

import (
	"container/list"
	"sync"
	"testing"

	"github.com/spacegrower/watermelon/infra/internal/context"
	"github.com/spacegrower/watermelon/infra/internal/definition"
)

func BenchmarkSetInto(b *testing.B) {
	c := context.Wrap(context.Background())

	for n := 0; n < b.N; n++ {
		SetInto(c, b.N, b.N)
	}
}

func BenchmarkSyncMap(b *testing.B) {
	var store sync.Map
	for n := 0; n < b.N; n++ {
		store.Store(b.N, b.N)
	}
}

func BenchmarkValue(b *testing.B) {
	c := context.Wrap(context.Background())
	SetInto(c, "1", 1)
	for n := 0; n < b.N; n++ {
		GetFrom(c, "1")
	}
}

func BenchmarkSyncMapLoad(b *testing.B) {
	var store sync.Map
	store.Store("1", 1)
	for n := 0; n < b.N; n++ {
		store.Load("1")
	}
}

func TestNilImpl(t *testing.T) {
	ctx := context.Background()
	SetInto(ctx, definition.CurrentRouterKey{}, nil)
	if currentRouter, ok := GetFrom(ctx, definition.CurrentRouterKey{}).(interface {
		Next() *list.Element
	}); ok {
		next(ctx, currentRouter.Next())
	}
}
