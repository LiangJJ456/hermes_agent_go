package orchestrator_test

import (
	"strings"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func TestUnmarshalGraph_RejectsBadEdgeCondition(t *testing.T) {
	data := []byte(`{
		"StartAt": "a",
		"Nodes": {"a": {"Type": "end"}, "b": {"Type": "end"}},
		"Edges": [{"From": "a", "To": "b", "Condition": "input.x + 1"}]
	}`)
	_, err := orchestrator.UnmarshalGraph(data)
	if err == nil {
		t.Fatal("expected error for invalid edge condition, got nil")
	}
	if !strings.Contains(err.Error(), "edge 0") {
		t.Fatalf("expected edge-condition validation error, got: %v", err)
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
	_, err := orchestrator.UnmarshalGraph(data)
	if err == nil {
		t.Fatal("expected error for invalid choice condition, got nil")
	}
	if !strings.Contains(err.Error(), "validate config") {
		t.Fatalf("expected choice-condition validation error, got: %v", err)
	}
}
