package agent

import (
	"context"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// eventTracer bridges orchestrator.Tracer to EventCallback.
type eventTracer struct {
	cb EventCallback
}

func (t *eventTracer) OnNodeStart(ctx context.Context, nodeID, nodeType string, _ interface{}) {
	if t.cb == nil {
		return
	}
	switch nodeType {
	case "tool":
		t.cb(Event{Type: EventToolStart, ToolName: nodeID, Timestamp: time.Now()})
	}
}

func (t *eventTracer) OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *orchestrator.NodeResult, err error) {
	if t.cb == nil {
		return
	}
	if err != nil {
		t.cb(Event{Type: EventError, Content: err.Error(), Timestamp: time.Now()})
		return
	}
	switch nodeType {
	case "tool":
		t.cb(Event{Type: EventToolEnd, ToolName: nodeID, Timestamp: time.Now()})
	}
}

func (t *eventTracer) OnStreamDelta(ctx context.Context, content string) {
	if t.cb == nil {
		return
	}
	t.cb(Event{Type: EventStreamDelta, Content: content, Timestamp: time.Now()})
}
