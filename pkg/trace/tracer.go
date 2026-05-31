package trace

import (
	"context"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// Tracer 统一可观测性接口
type Tracer interface {
	// StartNodeSpan 创建节点级 span，返回携带 span 信息的 ctx
	StartNodeSpan(ctx context.Context, nodeID, nodeType string) (context.Context, *Span)
	// EndNodeSpan 结束 span 并导出
	EndNodeSpan(span *Span, err error)
	// OnStreamDelta 流式输出事件（可选实现）
	OnStreamDelta(ctx context.Context, content string)
	// OnToolStart 工具开始执行时发出（用于实时反馈长任务，工具结束在 EndNodeSpan 中处理）
	OnToolStart(ctx context.Context, toolName, toolArgs string)
}

// --- NopTracer ---

// NopTracer 空实现
type NopTracer struct{}

func (*NopTracer) StartNodeSpan(ctx context.Context, nodeID, nodeType string) (context.Context, *Span) {
	return StartSpan(ctx, "node:"+nodeID, nodeType)
}
func (*NopTracer) EndNodeSpan(span *Span, err error)                    { span.End(err) }
func (*NopTracer) OnStreamDelta(_ context.Context, _ string)            {}
func (*NopTracer) OnToolStart(_ context.Context, _ string, _ string)    {}

// --- LocalTracer: 本地 JSON 日志输出 ---

// LocalTracer 将 span 输出到结构化日志，支持收集整条 trace
type LocalTracer struct {
	spans []*Span
	mu    sync.Mutex
}

func NewLocalTracer() *LocalTracer {
	return &LocalTracer{}
}

func (t *LocalTracer) StartNodeSpan(ctx context.Context, nodeID, nodeType string) (context.Context, *Span) {
	return StartSpan(ctx, "node:"+nodeID, nodeType)
}

func (t *LocalTracer) EndNodeSpan(span *Span, err error) {
	span.End(err)
	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()

	// 导出到日志
	attrs := make([]any, 0, 14)
	attrs = append(attrs,
		"trace_id", span.TraceID,
		"span_id", span.SpanID,
		"parent_id", span.ParentID,
		"name", span.Name,
		"node_type", span.NodeType,
		"duration_ms", span.Duration().Milliseconds(),
	)
	if span.Status == SpanError {
		attrs = append(attrs, "error", span.Error)
		log.Warn("span.end", attrs...)
	} else {
		// Append selected attributes
		span.mu.Lock()
		for k, v := range span.Attributes {
			attrs = append(attrs, k, v)
		}
		span.mu.Unlock()
		log.Info("span.end", attrs...)
	}
}

func (t *LocalTracer) OnStreamDelta(_ context.Context, _ string)         {}
func (t *LocalTracer) OnToolStart(_ context.Context, _ string, _ string) {}

// Spans 返回已收集的所有 span（用于测试或 trace 导出）
func (t *LocalTracer) Spans() []*Span {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := make([]*Span, len(t.spans))
	copy(cp, t.spans)
	return cp
}

// --- CompositeTracer: 多 tracer 扇出 ---

// CompositeTracer 将事件广播到多个 tracer
type CompositeTracer struct {
	tracers []Tracer
}

func NewCompositeTracer(tracers ...Tracer) *CompositeTracer {
	return &CompositeTracer{tracers: tracers}
}

func (c *CompositeTracer) StartNodeSpan(ctx context.Context, nodeID, nodeType string) (context.Context, *Span) {
	// 只创建一个 span（由第一个 tracer 创建），所有 tracer 共享
	if len(c.tracers) == 0 {
		return StartSpan(ctx, "node:"+nodeID, nodeType)
	}
	return c.tracers[0].StartNodeSpan(ctx, nodeID, nodeType)
}

func (c *CompositeTracer) EndNodeSpan(span *Span, err error) {
	for _, t := range c.tracers {
		t.EndNodeSpan(span, err)
	}
}

func (c *CompositeTracer) OnStreamDelta(ctx context.Context, content string) {
	for _, t := range c.tracers {
		t.OnStreamDelta(ctx, content)
	}
}

func (c *CompositeTracer) OnToolStart(ctx context.Context, toolName, toolArgs string) {
	for _, t := range c.tracers {
		t.OnToolStart(ctx, toolName, toolArgs)
	}
}
