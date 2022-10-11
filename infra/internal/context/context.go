package context

import (
	"context"
	"sync"
	"time"
)

func Background() context.Context {
	return &Context{
		ctx: context.Background(),
	}
}

func Wrap(ctx context.Context) context.Context {
	return &Context{
		ctx: ctx,
	}
}

type Context struct {
	ctx   context.Context
	store sync.Map
}

func (c *Context) Deadline() (deadline time.Time, ok bool) {
	return c.ctx.Deadline()
}

func (c *Context) Done() <-chan struct{} {
	return c.ctx.Done()
}

func (c *Context) Err() error {
	return c.ctx.Err()
}

func (c *Context) Value(key any) any {
	if value, exist := c.store.Load(key); exist {
		return value
	}
	return c.ctx.Value(key)
}

func (c *Context) Set(key, val any) {
	c.store.Store(key, val)
}
