package definition

type GrpcRequestType string

const (
	UnaryRequest  GrpcRequestType = "unary"
	StreamRequest GrpcRequestType = "stream"
)
