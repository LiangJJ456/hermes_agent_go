package executor

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

func TestRouteDynamicNext(t *testing.T) {
	result := &orchestrator.NodeResult{Next: "target"}
	next, err := Route(context.Background(), "src", result, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != "target" {
		t.Fatalf("expected 'target', got %q", next)
	}
}

func TestRouteByEdge(t *testing.T) {
	edges := []orchestrator.EdgeSpec{
		{From: "src", To: "dst", Priority: 0},
	}
	result := &orchestrator.NodeResult{Status: orchestrator.StatusContinue}
	next, err := Route(context.Background(), "src", result, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != "dst" {
		t.Fatalf("expected 'dst', got %q", next)
	}
}

func TestRoutePriority(t *testing.T) {
	edges := []orchestrator.EdgeSpec{
		{From: "src", To: "low", Priority: 10},
		{From: "src", To: "high", Priority: 0},
	}
	result := &orchestrator.NodeResult{Status: orchestrator.StatusContinue}
	next, err := Route(context.Background(), "src", result, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != "high" {
		t.Fatalf("expected high-priority edge 'high', got %q", next)
	}
}

func TestRouteNoEdge(t *testing.T) {
	result := &orchestrator.NodeResult{Status: orchestrator.StatusContinue}
	_, err := Route(context.Background(), "src", result, nil, nil)
	if err == nil {
		t.Fatal("expected error when no edge and no dynamic Next")
	}
}

func TestRoute_ConditionalEdgeFirstMatchWins(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{"has_tool_calls": true},
	}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
		{From: "n", To: "end", Priority: 1}, // unconditional fallback
	}
	next, err := Route(context.Background(), "n", result, edges, ec)
	if err != nil {
		t.Fatal(err)
	}
	if next != "tools" {
		t.Fatalf("expected 'tools', got %q", next)
	}
}

func TestRoute_FallsThroughToUnconditional(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{"has_tool_calls": false},
	}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
		{From: "n", To: "end", Priority: 1},
	}
	next, err := Route(context.Background(), "n", result, edges, ec)
	if err != nil {
		t.Fatal(err)
	}
	if next != "end" {
		t.Fatalf("expected 'end', got %q", next)
	}
}

func TestRoute_NoMatchingConditionalEdgeErrors(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{"has_tool_calls": false},
	}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
	}
	if _, err := Route(context.Background(), "n", result, edges, ec); err == nil {
		t.Fatal("expected error when no conditional edge matches and no fallback")
	}
}

func TestRoute_DynamicNextOverridesEdges(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: "explicit"}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
	}
	next, err := Route(context.Background(), "n", result, edges, ec)
	if err != nil {
		t.Fatal(err)
	}
	if next != "explicit" {
		t.Fatalf("expected 'explicit', got %q", next)
	}
}
