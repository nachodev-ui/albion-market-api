package observability

import "context"

type correlationIDContextKey struct{}

func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDContextKey{}, correlationID)
}

func CorrelationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationIDContextKey{}).(string)
	return value
}
