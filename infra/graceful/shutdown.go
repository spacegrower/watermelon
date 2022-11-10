package graceful

import (
	"sort"
	"sync"
)

type shutdown struct {
	once        sync.Once
	handlers    []func()
	preHandlers []func()
}

var shutDownHandlers *shutdown

func init() {
	shutDownHandlers = &shutdown{}
}

func RegisterShutDownHandlers(f ...func()) {
	shutDownHandlers.handlers = append(shutDownHandlers.handlers, f...)
}

func RegisterPreShutDownHandlers(f ...func()) {
	shutDownHandlers.preHandlers = append(shutDownHandlers.preHandlers, f...)
}

func ShutDown() {
	shutDownHandlers.once.Do(func() {
		sort.SliceStable(shutDownHandlers.preHandlers, func(i, j int) bool {
			return true
		})
		for _, f := range shutDownHandlers.preHandlers {
			f()
		}

		for _, f := range shutDownHandlers.handlers {
			f()
		}
	})
}
