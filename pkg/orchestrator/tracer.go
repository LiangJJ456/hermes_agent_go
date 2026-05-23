package orchestrator

import (
	"context"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/trace"
)

// Tracer receives execution events. Implementations can log, emit metrics,
// or bridge to legacy EventCallback.
//
// Deprecated: 保留旧接口用于兼容，新代码应使用 trace.Tracer
type Tracer interface {
	OnNodeStart(ctx context.Context, nodeID, nodeType string, input interface{})
	OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *NodeResult, err error)
	OnStreamDelta(ctx context.Context, content string)
}

// NopTracer is a no-op tracer for when observability is not needed.
type NopTracer struct{}

func (*NopTracer) OnNodeStart(_ context.Context, _, _ string, _ interface{}) {}
func (*NopTracer) OnNodeEnd(_ context.Context, _, _ string, _ *NodeResult, _ error) {}
func (*NopTracer) OnStreamDelta(_ context.Context, _ string)                        {}

// LegacyBridge 将旧的 orchestrator.Tracer 适配为 trace.Tracer
type LegacyBridge struct {
	Old Tracer
}

func (b *LegacyBridge) StartNodeSpan(ctx context.Context, nodeID, nodeType string) (context.Context, *trace.Span) {
	ctx, span := trace.StartSpan(ctx, "node:"+nodeID, nodeType)
	b.Old.OnNodeStart(ctx, nodeID, nodeType, nil)
	return ctx, span
}

func (b *LegacyBridge) EndNodeSpan(span *trace.Span, err error) {
	span.End(err)
	// LegacyBridge 不再调用 OnNodeEnd (已在外部调用)
}

func (b *LegacyBridge) OnStreamDelta(ctx context.Context, content string) {
	b.Old.OnStreamDelta(ctx, content)
}
