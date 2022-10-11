package wlog

import (
	"strings"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Config struct {
	Name         string
	Level        Level
	File         string
	RotateConfig *RotateConfig
	Wrappers     []func(*zap.Logger) *zap.Logger
}

// RotateConfig config log rotate
type RotateConfig struct {
	Compress   bool
	MaxAge     int
	MaxSize    int
	MaxBackups int
}

type Level uint

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	PanicLevel
)

func (l Level) zaplevel() zapcore.Level {
	switch l {
	case DebugLevel:
		return zapcore.DebugLevel
	case InfoLevel:
		return zapcore.InfoLevel
	case WarnLevel:
		return zapcore.WarnLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	case PanicLevel:
		return zapcore.PanicLevel
	}

	return zapcore.InfoLevel
}

// ParseLevel parse level string
func ParseLevel(str string) Level {
	switch strings.ToLower(str) {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "panic":
		return PanicLevel
	}

	// Default log level
	return InfoLevel
}
