package wlog

import (
	"os"
	"runtime"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/spacegrower/watermelon/infra/graceful"
)

// Logger is the log interface
type Logger interface {
	With(fields ...zap.Field) Logger
	Debug(msg string, fields ...zap.Field)
	Info(msg string, fields ...zap.Field)
	Warn(msg string, fields ...zap.Field)
	Error(msg string, fields ...zap.Field)
	Panic(msg string, fields ...zap.Field)
}

// NewLogger is a function to create new zap logger
// this logger implement the Logger interface
func NewLogger(cfg *Config) *wrappedLogger {

	var writer zapcore.WriteSyncer
	if cfg.File == "" {
		// default: write to stdout
		writer = zapcore.AddSync(os.Stdout)
	} else {
		if cfg.RotateConfig == nil {
			cfg.RotateConfig = &RotateConfig{
				Compress:   true,
				MaxAge:     24,
				MaxSize:    100,
				MaxBackups: 1,
			}
		}
		writer = zapcore.AddSync(&lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.RotateConfig.MaxSize,
			MaxBackups: cfg.RotateConfig.MaxBackups,
			MaxAge:     cfg.RotateConfig.MaxAge,
			Compress:   cfg.RotateConfig.Compress,
		})
	}

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "name",
		MessageKey:     "msg",
		StacktraceKey:  "stack",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeName:     zapcore.FullNameEncoder,
		EncodeCaller:   zapcore.FullCallerEncoder,
	}

	// 设置日志级别
	atomicLevel := zap.NewAtomicLevel()
	atomicLevel.SetLevel(cfg.Level.zaplevel())

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		writer,
		atomicLevel,
	)

	// init fields
	if cfg.Name == "" {
		cfg.Name = "watermelon"
	}
	initfields := zap.Fields(zap.String("service", cfg.Name), zap.String("runtime", runtime.Version()))
	logger := zap.New(core, zap.AddStacktrace(zapcore.PanicLevel), initfields)

	// wrap logger
	for _, wrapper := range cfg.Wrappers {
		logger = wrapper(logger)
	}

	graceful.RegisterShutDownHandlers(func() {
		logger.Sync()
	})

	return Wrapper(logger)
}
