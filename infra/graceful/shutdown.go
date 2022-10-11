package graceful

import "sync"

type shutdown struct {
	once     sync.Once
	handlers []func()
}

var shutDownHandlers *shutdown

func init() {
	shutDownHandlers = &shutdown{}
}

func RegisterShutDownHandlers(f ...func()) {
	shutDownHandlers.handlers = append(shutDownHandlers.handlers, f...)
}

func ShutDown() {
	shutDownHandlers.once.Do(func() {
		for _, f := range shutDownHandlers.handlers {
			f()
		}
	})
}
