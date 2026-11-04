package aihelper

import "context"

type reasoningKey struct{}

func WithReasoningCallback(ctx context.Context, cb StreamCallback) context.Context {
	return context.WithValue(ctx, reasoningKey{}, cb)
}
func emitReasoning(ctx context.Context, text string) {
	if cb, ok := ctx.Value(reasoningKey{}).(StreamCallback); ok {
		cb(text)
	}
}
