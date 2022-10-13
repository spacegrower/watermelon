package middleware

import (
	"context"

	"google.golang.org/grpc"

	cst "github.com/spacegrower/watermelon/infra/definition"
	"github.com/spacegrower/watermelon/infra/internal/definition"
)

// GetFullMethodFrom a function to return grpc full method
func GetFullMethodFrom(ctx context.Context) string {
	return GetFrom(ctx, definition.FullMethodKey{}).(string)
}

// GetRequestFrom a function to return grpc request body
func GetRequestFrom(ctx context.Context) any {
	return GetFrom(ctx, definition.RequestKey{})
}

// GetResponseFrom a function to return grpc handler response
func GetResponseFrom(ctx context.Context) any {
	return GetFrom(ctx, definition.ResponseKey{})
}

// GetUnaryHandlerFrom a function to return grpc unary handler
func GetUnaryHandlerFrom(ctx context.Context) grpc.UnaryHandler {
	return GetFrom(ctx, definition.UnaryHandlerKey{}).(grpc.UnaryHandler)
}

// GetStreamHandlerFrom a function to return grpc stream handler
func GetStreamHandlerFrom(ctx context.Context) func() error {
	return GetFrom(ctx, definition.StreamHandlerKey{}).(func() error)
}

// GetGrpcRequestTypeFrom a function to return grpc request type
// stream | unary
func GetGrpcRequestTypeFrom(ctx context.Context) cst.GrpcRequestType {
	return GetFrom(ctx, definition.RequestTypeKey{}).(cst.GrpcRequestType)
}
