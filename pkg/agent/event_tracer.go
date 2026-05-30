package agent

import (
	"context"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/trace"
)

// eventTracer bridges trace.Tracer to EventCallback (backward compat).
type eventTracer struct {
	cb     EventCallback
	inner  trace.Tracer // optional: delegate for span storage/export
}

// newEventTracer creates a tracer that emits Events and optionally delegates to an inner tracer.
func newEventTracer(cb EventCallback, inner trace.Tracer) *eventTracer {
	if inner == nil {
		inner = trace.NewLocalTracer()
	}
	return &eventTracer{cb: cb, inner: inner}
}

func (t *eventTracer) StartNodeSpan(ctx context.Context, nodeID, nodeType string) (context.Context, *trace.Span) {
	return t.inner.StartNodeSpan(ctx, nodeID, nodeType)
}

func (t *eventTracer) EndNodeSpan(span *trace.Span, err error) {
	t.inner.EndNodeSpan(span, err)
	if t.cb == nil {
		return
	}
	if err != nil {
		t.cb(Event{Type: EventError, Content: err.Error(), Timestamp: time.Now()})
		return
	}
	if span.NodeType == "tool" {
		toolName := span.Name
		var toolArgs string
		if n, ok := span.GetAttribute("tool_name"); ok {
			if s, ok := n.(string); ok && s != "" {
				toolName = s
			}
		}
		if a, ok := span.GetAttribute("tool_args"); ok {
			if s, ok := a.(string); ok {
				toolArgs = s
			}
		}
		t.cb(Event{Type: EventToolEnd, ToolName: toolName, ToolArgs: toolArgs, Timestamp: time.Now()})
	}
}

func (t *eventTracer) OnStreamDelta(ctx context.Context, content string) {
	t.inner.OnStreamDelta(ctx, content)
	if t.cb != nil {
		t.cb(Event{Type: EventStreamDelta, Content: content, Timestamp: time.Now()})
	}
}

// OnToolStart fires when a tool actually begins executing (before Invoker.Invoke).
// This is the real-time signal users need for long-running tools — EndNodeSpan
// can't be used because it fires only after the tool completes.
func (t *eventTracer) OnToolStart(ctx context.Context, toolName, toolArgs string) {
	t.inner.OnToolStart(ctx, toolName, toolArgs)
	if t.cb != nil {
		t.cb(Event{Type: EventToolStart, ToolName: toolName, ToolArgs: toolArgs, Timestamp: time.Now()})
	}
}
