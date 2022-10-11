package wlog

import (
	"sync"

	"go.uber.org/zap"
)

var (
	logger       Logger
	globalLocker sync.Mutex
)

func init() {
	logger = Wrapper(zap.NewExample())
}

type wrappedLogger struct {
	*zap.Logger
}

func (l *wrappedLogger) With(fields ...zap.Field) Logger {
	copy := l.Logger.With(fields...)
	return Wrapper(copy)
}

// Wrapper is a function to parse zap.Logger to Logger interface
func Wrapper(l *zap.Logger) *wrappedLogger {
	return &wrappedLogger{
		Logger: l,
	}
}

// SetGlobalLogger is a function to replace global logger object
func SetGlobalLogger(l Logger) {
	globalLocker.Lock()
	defer globalLocker.Unlock()

	logger = l
}

// Debug logs a message at DebugLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func Debug(msg string, fields ...zap.Field) {
	logger.Debug(msg, fields...)
}

// Debug logs a message at DebugLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func Info(msg string, fields ...zap.Field) {
	logger.Info(msg, fields...)
}

// Debug logs a message at DebugLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func Warn(msg string, fields ...zap.Field) {
	logger.Warn(msg, fields...)
}

// Debug logs a message at DebugLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func Error(msg string, fields ...zap.Field) {
	logger.Error(msg, fields...)
}

// Debug logs a message at DebugLevel. The message includes any fields passed
// at the log site, as well as any fields accumulated on the logger.
func Panic(msg string, fields ...zap.Field) {
	logger.Panic(msg, fields...)
}

// With is a function to return the Logger with some preset fields
func With(fields ...zap.Field) Logger {
	return logger.With(fields...)
}
