package middleware

import (
	"context"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type contextKey string

const (
	loggerKey    contextKey = "logger"
	sessionIDKey contextKey = "sessionId"
	supiKey      contextKey = "supi"
	traceIDKey   contextKey = "traceId"
)

// NewLogger creates a zap logger with the given level and format
func NewLogger(level, format string) (*zap.Logger, error) {
	lvl, err := zapcore.ParseLevel(level)
	if err != nil {
		lvl = zapcore.InfoLevel
	}

	cfg := zap.NewProductionConfig()
	cfg.Level = zap.NewAtomicLevelAt(lvl)

	if format == "console" {
		cfg.Encoding = "console"
		cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	return cfg.Build()
}

// WithSessionID attaches a session ID to the context logger
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// WithSupi attaches a SUPI to the context logger
func WithSupi(ctx context.Context, supi string) context.Context {
	return context.WithValue(ctx, supiKey, supi)
}

// WithLogger attaches a logger to the context
func WithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// LoggerFromContext retrieves the logger from context, or returns a nop logger
func LoggerFromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(loggerKey).(*zap.Logger); ok && l != nil {
		// Enrich with context values
		fields := []zap.Field{}
		if sid, ok := ctx.Value(sessionIDKey).(string); ok && sid != "" {
			fields = append(fields, zap.String("sessionId", sid))
		}
		if supi, ok := ctx.Value(supiKey).(string); ok && supi != "" {
			fields = append(fields, zap.String("supi", supi))
		}
		if len(fields) > 0 {
			return l.With(fields...)
		}
		return l
	}
	return zap.NewNop()
}

// GrpcLoggingInterceptor is a unary server interceptor that logs requests
func GrpcLoggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		ctx = WithLogger(ctx, logger)

		resp, err := handler(ctx, req)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		logger.Info("grpc request",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", time.Since(start)),
			zap.String("code", code.String()),
			zap.Error(err),
		)

		return resp, err
	}
}

// GrpcStreamLoggingInterceptor is a stream server interceptor that logs streams
func GrpcStreamLoggingInterceptor(logger *zap.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		err := handler(srv, ss)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		logger.Info("grpc stream",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", time.Since(start)),
			zap.String("code", code.String()),
			zap.Error(err),
		)

		return err
	}
}
