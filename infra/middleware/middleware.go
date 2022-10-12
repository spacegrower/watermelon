package middleware

import (
	"context"

	wctx "github.com/spacegrower/watermelon/infra/internal/context"
	"github.com/spacegrower/watermelon/infra/internal/definition"
	"github.com/spacegrower/watermelon/infra/wlog"
)

// Middleware abstract grpc interceptor
type Middleware func(context.Context) error

// SetInto a function to save the value into watermelon context
func SetInto(c context.Context, key, val any) {
	if ctx, ok := c.Value(definition.ContextKey{}).(*wctx.Context); ok {
		ctx.Set(key, val)
	} else {
		wlog.Panic("middleware: not found github.com/spacegrower/watermelon/infra/internal/context.Context from the given context")
	}
}

// GetFrom a function to fetch the value from watermelon context
func GetFrom(c context.Context, key any) any {
	return c.Value(key)
}

// Next a function to handle next middleware.
// avoid using the same instance(context) for concurrent scenarios
func Next(ctx context.Context) error {
	currentRouter, ok := GetFrom(ctx, definition.CurrentRouterKey{}).(Router)

	if ok && !currentRouter.Next().IsNil() {
		return currentRouter.Next().Deep(ctx)
	}
	return nil
}
