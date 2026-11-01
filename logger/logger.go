package logger

import (
	"context"
	"os"

	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	Log *zap.Logger
)

func init() {

	var logLevel zapcore.Level

	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		logLevel = zap.DebugLevel
	case "info":
		logLevel = zap.InfoLevel
	case "warn":
		logLevel = zap.WarnLevel
	case "error":
		logLevel = zap.ErrorLevel
	default:
		logLevel = zap.InfoLevel
	}

	logConfig := zap.Config{
		OutputPaths:   []string{"stdout"},
		Encoding:      "json",
		Level:         zap.NewAtomicLevelAt(logLevel),
		InitialFields: map[string]interface{}{"serviceName": os.Getenv("SERVICE_NAME")},
		EncoderConfig: zapcore.EncoderConfig{
			MessageKey:   "msg",
			LevelKey:     "level",
			EncodeLevel:  zapcore.LowercaseLevelEncoder,
			TimeKey:      "time",
			EncodeTime:   zapcore.ISO8601TimeEncoder,
			CallerKey:    "caller",
			EncodeCaller: zapcore.ShortCallerEncoder,
		},
	}

	var err error

	Log, err = logConfig.Build()

	if err != nil {
		panic(err)
	}

}

func TraceLogger(ctx context.Context) *zap.Logger {
	span, ok := tracer.SpanFromContext(ctx)
	if !ok {
		return Log.With(zap.String("trace_debug", "no_span"))
	}

	spanCtx := span.Context()

	return Log.With(
		zap.String("traceId", spanCtx.TraceID()),
	)
}
