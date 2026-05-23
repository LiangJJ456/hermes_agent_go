package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type ctxKey struct{}

// SpanContext 携带在 context 中的链路信息
type SpanContext struct {
	TraceID  string
	SpanID   string
	ParentID string
}

// NewTraceID 生成 128-bit trace ID
func NewTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSpanID 生成 64-bit span ID
func NewSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WithSpanContext 将 SpanContext 注入 ctx
func WithSpanContext(ctx context.Context, sc SpanContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, sc)
}

// FromContext 从 ctx 中提取 SpanContext
func FromContext(ctx context.Context) (SpanContext, bool) {
	sc, ok := ctx.Value(ctxKey{}).(SpanContext)
	return sc, ok
}
