package trace

import (
	"context"
	"log/slog"
)

// TracedHandler 包装 slog.Handler，自动从 ctx 注入 trace_id/span_id
type TracedHandler struct {
	inner slog.Handler
}

// NewTracedHandler 创建带 trace 注入的 slog Handler
func NewTracedHandler(inner slog.Handler) *TracedHandler {
	return &TracedHandler{inner: inner}
}

func (h *TracedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *TracedHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc, ok := FromContext(ctx); ok && sc.TraceID != "" {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID),
			slog.String("span_id", sc.SpanID),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *TracedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TracedHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *TracedHandler) WithGroup(name string) slog.Handler {
	return &TracedHandler{inner: h.inner.WithGroup(name)}
}
