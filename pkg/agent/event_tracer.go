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
	ctx, span := t.inner.StartNodeSpan(ctx, nodeID, nodeType)
	if t.cb != nil {
		switch nodeType {
		case "tool":
			t.cb(Event{Type: EventToolStart, ToolName: nodeID, Timestamp: time.Now()})
		}
	}
	return ctx, span
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
	switch span.NodeType {
	case "tool":
		t.cb(Event{Type: EventToolEnd, ToolName: span.Name, Timestamp: time.Now()})
	}
}

func (t *eventTracer) OnStreamDelta(ctx context.Context, content string) {
	t.inner.OnStreamDelta(ctx, content)
	if t.cb != nil {
		t.cb(Event{Type: EventStreamDelta, Content: content, Timestamp: time.Now()})
	}
}
