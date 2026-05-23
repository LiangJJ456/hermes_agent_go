package orchestrator

import "context"

// Tracer receives execution events. Implementations can log, emit metrics,
// or bridge to legacy EventCallback.
type Tracer interface {
	OnNodeStart(ctx context.Context, nodeID, nodeType string, input interface{})
	OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *NodeResult, err error)
	OnStreamDelta(ctx context.Context, content string)
}

// NopTracer is a no-op tracer for when observability is not needed.
type NopTracer struct{}

func (NopTracer) OnNodeStart(ctx context.Context, nodeID, nodeType string, input interface{}) {}
func (NopTracer) OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *NodeResult, err error) {}
func (NopTracer) OnStreamDelta(ctx context.Context, content string) {}
