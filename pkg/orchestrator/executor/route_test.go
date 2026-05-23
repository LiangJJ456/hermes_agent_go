package executor

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
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
