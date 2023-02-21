package utils

import (
	"context"
	"errors"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
)

func PointerValue[T int |
	uint |
	uint64 |
	uint32 |
	int32 |
	int8 |
	uint8 |
	float32 |
	float64 |
	string |
	bool |
	[]byte |
	[]rune](i T) *T {
	// if reflect.DeepEqual(i, reflect.Zero(reflect.TypeOf(i)).Interface()) {
	// 	return nil
	// }
	return &i
}

// GetHostIP return local IP
func GetHostIP() (string, error) {
	addrs, err := net.InterfaceAddrs()

	if err != nil {
		return "", err
	}

	for _, address := range addrs {
		// 检查ip地址判断是否回环地址
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}

		}
	}

	return "", errors.New("Can not find the host ip address!")
}

// NewContextWithSignal
func NewContextWithSignal(signals ...os.Signal) context.Context {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, signals...)
	ctx, cancel := context.WithCancel(context.TODO())

	go func() {
		<-ch
		cancel()
	}()
	return ctx
}

// GetEnvWithDefault a function to return system env value
// return default value when env is empty
func GetEnvWithDefault[T string | int | int32](env string, def T, parse func(val string) (T, error)) T {
	if val := os.Getenv(env); val != "" {
		if res, err := parse(val); err == nil {
			return res
		}
	}
	return def
}

// PathJoin join file path with slash
func PathJoin(elem ...string) string {
	p := filepath.ToSlash(filepath.Join(elem...))
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}
