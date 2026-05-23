package trace

import (
	"context"
	"errors"
	"testing"
)

func TestSpanContext_Propagation(t *testing.T) {
	ctx := context.Background()

	// Root span
	ctx, root := StartSpan(ctx, "root", "agent")
	if root.TraceID == "" {
		t.Fatal("root span should have a TraceID")
	}
	if root.ParentID != "" {
		t.Fatal("root span should have empty ParentID")
	}

	// Child span inherits TraceID
	ctx2, child := StartSpan(ctx, "child", "llm")
	if child.TraceID != root.TraceID {
		t.Fatalf("child TraceID %s != root TraceID %s", child.TraceID, root.TraceID)
	}
	if child.ParentID != root.SpanID {
		t.Fatalf("child ParentID %s != root SpanID %s", child.ParentID, root.SpanID)
	}

	// Grandchild
	_, grandchild := StartSpan(ctx2, "grandchild", "tool")
	if grandchild.TraceID != root.TraceID {
		t.Fatal("grandchild should share root TraceID")
	}
	if grandchild.ParentID != child.SpanID {
		t.Fatalf("grandchild ParentID %s != child SpanID %s", grandchild.ParentID, child.SpanID)
	}
}

func TestSpan_End(t *testing.T) {
	ctx := context.Background()
	_, span := StartSpan(ctx, "test", "tool")

	span.SetAttribute("key", "value")
	span.End(nil)

	if span.Status != SpanOK {
		t.Fatal("expected SpanOK")
	}
	if span.Duration() <= 0 {
		t.Fatal("expected positive duration")
	}
	if span.Attributes["key"] != "value" {
		t.Fatal("attribute not set")
	}
}

func TestSpan_EndWithError(t *testing.T) {
	ctx := context.Background()
	_, span := StartSpan(ctx, "test", "llm")

	span.End(errors.New("timeout"))

	if span.Status != SpanError {
		t.Fatal("expected SpanError")
	}
	if span.Error != "timeout" {
		t.Fatalf("expected 'timeout', got %q", span.Error)
	}
}

func TestLocalTracer_CollectsSpans(t *testing.T) {
	tracer := NewLocalTracer()
	ctx := context.Background()

	ctx, span1 := tracer.StartNodeSpan(ctx, "llm_call", "llm")
	span1.SetAttribute("model", "gpt-4")
	tracer.EndNodeSpan(span1, nil)

	_, span2 := tracer.StartNodeSpan(ctx, "tool_search", "tool")
	tracer.EndNodeSpan(span2, errors.New("not found"))

	spans := tracer.Spans()
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if spans[0].Name != "node:llm_call" {
		t.Fatalf("expected 'node:llm_call', got %q", spans[0].Name)
	}
	if spans[1].Status != SpanError {
		t.Fatal("second span should be error")
	}
	// Same trace
	if spans[0].TraceID != spans[1].TraceID {
		t.Fatal("spans should share traceID")
	}
	// Parent chain
	if spans[1].ParentID != spans[0].SpanID {
		t.Fatalf("span2 parent should be span1: %s != %s", spans[1].ParentID, spans[0].SpanID)
	}
}

func TestFromContext_Empty(t *testing.T) {
	sc, ok := FromContext(context.Background())
	if ok {
		t.Fatal("expected ok=false for empty context")
	}
	if sc.TraceID != "" {
		t.Fatal("expected empty TraceID")
	}
}
