package orchestrator

import (
	"context"
	"testing"
)

func TestUnmarshalGraph(t *testing.T) {
	// Register a test type
	RegisterNodeType("end", &testEndRunner{}, &struct{ Status string }{})

	data := []byte(`{
		"StartAt": "done",
		"MaxSteps": 10,
		"Nodes": {
			"done": {"Type": "end", "Config": {"Status": "success"}}
		},
		"Edges": []
	}`)

	g, err := UnmarshalGraph(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.StartAt != "done" {
		t.Fatalf("expected StartAt='done', got %q", g.StartAt)
	}
	if g.MaxSteps != 10 {
		t.Fatalf("expected MaxSteps=10, got %d", g.MaxSteps)
	}
	node, ok := g.Nodes["done"]
	if !ok {
		t.Fatal("expected 'done' node")
	}
	if node.Type != "end" {
		t.Fatalf("expected Type='end', got %q", node.Type)
	}
	if node.ParsedConfig == nil {
		t.Fatal("expected ParsedConfig to be set")
	}
}

func TestUnmarshalGraph_RejectsBadEdgeCondition(t *testing.T) {
	data := []byte(`{
		"StartAt": "a",
		"Nodes": {"a": {"Type": "end"}, "b": {"Type": "end"}},
		"Edges": [{"From": "a", "To": "b", "Condition": "input.x + 1"}]
	}`)
	if _, err := UnmarshalGraph(data); err == nil {
		t.Fatal("expected error for invalid edge condition, got nil")
	}
}

func TestUnmarshalGraph_RejectsBadChoiceCondition(t *testing.T) {
	data := []byte(`{
		"StartAt": "c",
		"Nodes": {
			"c": {"Type": "choice", "Config": {"Choices": [{"Condition": "foo(bar)", "Next": "end"}], "Default": "end"}},
			"end": {"Type": "end"}
		}
	}`)
	if _, err := UnmarshalGraph(data); err == nil {
		t.Fatal("expected error for invalid choice condition, got nil")
	}
}

type testEndRunner struct{}

func (r *testEndRunner) Run(ctx context.Context, node *NodeSpec, input interface{}, execCtx interface{}) (*NodeResult, error) {
	return &NodeResult{Status: StatusEnd}, nil
}
