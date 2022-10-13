package preset

import (
	"context"

	"google.golang.org/grpc"

	cst "github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/internal/definition"
	"github.com/spacegrower/watermelon/infra/middleware"
)

// SetUnaryHandlerInto a function to save the grpc unary handler into watermelon context
func SetUnaryHandlerInto(ctx context.Context, handler grpc.UnaryHandler) {
	middleware.SetInto(ctx, definition.UnaryHandlerKey{}, handler)
}

// SetStreamHandlerInto a function to save the grpc stream handler into watermelon context
func SetStreamHandlerInto(ctx context.Context, handle func() error) {
	middleware.SetInto(ctx, definition.StreamHandlerKey{}, handle)
}

// SetRequestInto a function to save the grpc request body into watermelon context
func SetRequestInto(ctx context.Context, req any) {
	middleware.SetInto(ctx, definition.RequestKey{}, req)
}

// SetFullMethodInto a function to save the grpc full method name into watermelon context
func SetFullMethodInto(ctx context.Context, fullMethod string) {
	middleware.SetInto(ctx, definition.FullMethodKey{}, fullMethod)
}

// SetGrpcRequestTypeInto a function to save the grpc request type into watermelon context
// stream | unary
func SetGrpcRequestTypeInto(ctx context.Context, _type cst.GrpcRequestType) {
	middleware.SetInto(ctx, definition.RequestTypeKey{}, _type)
}
