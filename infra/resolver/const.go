package resolver

import "google.golang.org/grpc/resolver"

// resolver return a fake address for fail fast, do not block client rpc until deadline
var (
	NilAddress = resolver.Address{
		Addr: "nil address",
	}
)
