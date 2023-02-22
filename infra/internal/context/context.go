package context

import (
	"context"
	"sync"
)

func Background() context.Context {
	return &Context{
		Context: context.Background(),
	}
}

func Wrap(ctx context.Context) context.Context {
	c := &Context{
		Context: ctx,
	}
	// c.Set(definition.ContextKey{}, c)
	return c
}

type Context struct {
	context.Context
	store sync.Map
}

func (c *Context) Value(key any) any {
	if value, exist := c.store.Load(key); exist {
		return value
	}
	return c.Context.Value(key)
}

func (c *Context) Set(key, val any) {
	c.store.Store(key, val)
}
