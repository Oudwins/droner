package logbuf

import "context"

type contextKey struct{}

func WithContext(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) *Logger {
	logger, ok := ctx.Value(contextKey{}).(*Logger)
	if !ok {
		return nil
	}
	return logger
}
