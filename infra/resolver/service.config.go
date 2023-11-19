package resolver

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"google.golang.org/grpc/codes"

	"github.com/spacegrower/watermelon/infra/balancer"
	"github.com/spacegrower/watermelon/infra/utils"
)

type Resolver interface {
	GenerateTarget(serviceName string) string
}

var defaultGrpcServiceConfig = fmt.Sprintf(`{"balancer":"%s"}`, balancer.WeightRobinName)

// GetDefaultGrpcServiceConfig is a function to return default grpc service config
// as specified here https://github.com/grpc/grpc/blob/master/doc/service_config.md
func GetDefaultGrpcServiceConfig() string {
	return defaultGrpcServiceConfig
}

type CustomizeServiceConfig struct {
	ServiceName          string                            `json:"service_name"`
	Balancer             string                            `json:"balancer"`
	Disabled             bool                              `json:"disabled"`
	MethodsConfig        map[string]*CustomizeMethodConfig `json:"methods_config"`
	DefaultMethodsConfig *CustomizeMethodConfig            `json:"default_methods_config"`
}

type CustomizeMethodConfig struct {
	RateLimit int `json:"rate_limit"`
	Timeout   int `json:"timeout"`
}

// defined follow https://github.com/grpc/grpc/blob/master/doc/service_config.md
// MethodConfig defines the configuration recommended by the service providers for a
// particular method.
type GrpcMethodConfig struct {
	Name []jsonName
	// WaitForReady indicates whether RPCs sent to this method should wait until
	// the connection is ready by default (!failfast). The value specified via the
	// gRPC client API will override the value set here.
	WaitForReady *bool
	// Timeout is the default timeout for RPCs sent to this method. The actual
	// deadline used will be the minimum of the value specified here and the value
	// set by the application via the gRPC client API.  If either one is not set,
	// then the other will be used.  If neither is set, then the RPC has no deadline.
	Timeout *string
	// MaxReqSize is the maximum allowed payload size for an individual request in a
	// stream (client->server) in bytes. The size which is measured is the serialized
	// payload after per-message compression (but before stream compression) in bytes.
	// The actual value used is the minimum of the value specified here and the value set
	// by the application via the gRPC client API. If either one is not set, then the other
	// will be used.  If neither is set, then the built-in default is used.
	MaxReqSize *int
	// MaxRespSize is the maximum allowed payload size for an individual response in a
	// stream (server->client) in bytes.
	MaxRespSize *int
	// RetryPolicy configures retry options for the method.
	RetryPolicy *GrpcRetryPolicy
}

// RetryPolicy defines the go-native version of the retry policy defined by the
// service config here:
// https://github.com/grpc/proposal/blob/master/A6-client-retries.md#integration-with-service-config
type GrpcRetryPolicy struct {
	// MaxAttempts is the maximum number of attempts, including the original RPC.
	//
	// This field is required and must be two or greater.
	MaxAttempts int

	// Exponential backoff parameters. The initial retry attempt will occur at
	// random(0, initialBackoff). In general, the nth attempt will occur at
	// random(0,
	//   min(initialBackoff*backoffMultiplier**(n-1), maxBackoff)).
	//
	// These fields are required and must be greater than zero.
	InitialBackoff    time.Duration
	MaxBackoff        time.Duration
	BackoffMultiplier float64

	// The set of status codes which may be retried.
	//
	// Status codes are specified as strings, e.g., "UNAVAILABLE".
	//
	// This field is required and must be non-empty.
	// Note: a set is used to store this for easy lookup.
	RetryableStatusCodes map[codes.Code]bool
}

type jsonName struct {
	Service *string
	Method  *string
}

type grpcServiceConfig struct {
	LoadBalancingPolicy *string
	LoadBalancingConfig []map[string]json.RawMessage
	MethodConfig        []GrpcMethodConfig
}

// ParseCustomizeToGrpcServiceConfig is a function to parse customize service config to grpc service config
func ParseCustomizeToGrpcServiceConfig(c *CustomizeServiceConfig) string {
	if c == nil {
		return defaultGrpcServiceConfig
	}
	result := &grpcServiceConfig{
		LoadBalancingPolicy: utils.PointerValue(c.Balancer),
	}

	for fullMethod, config := range c.MethodsConfig {
		dir, method := filepath.Split(fullMethod)
		_, service := filepath.Split(dir)

		result.MethodConfig = append(result.MethodConfig, GrpcMethodConfig{
			Name: []jsonName{
				{
					Service: &service,
					Method:  &method,
				},
			},
			Timeout: utils.PointerValue(fmt.Sprintf("%fs", float64(config.Timeout)/1000)),
		})
	}

	if c.DefaultMethodsConfig != nil {
		result.MethodConfig = append(result.MethodConfig, GrpcMethodConfig{
			Name: []jsonName{
				{
					Service: &c.ServiceName,
				},
			},
			Timeout: utils.PointerValue(fmt.Sprintf("%fs", float64(c.DefaultMethodsConfig.Timeout)/1000)),
		})
	}

	raw, _ := json.Marshal(result)
	return string(raw)
}

