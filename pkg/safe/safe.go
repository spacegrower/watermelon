package safe

import (
	"github.com/spacegrower/watermelon/infra/wlog"
	"go.uber.org/zap"
)

func Run(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			// todo log recover
			wlog.Error("panic", zap.Any("recover", r), zap.String("component", "safe"))
		}
	}()

	fn()
}
