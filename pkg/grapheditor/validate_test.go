package grapheditor

import (
	"strings"
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

const validGraph = `{
  "StartAt": "a",
  "Nodes": {
    "a": {"Type": "end", "Config": {"Status": "success"}},
    "b": {"Type": "end", "Config": {"Status": "success"}}
  },
  "Edges": [{"From": "a", "To": "b"}]
}`

func TestValidateGraph_Valid(t *testing.T) {
	resp := ValidateGraph([]byte(validGraph))
	if !resp.Valid {
		t.Fatalf("expected valid, got errors: %+v", resp.Errors)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("expected no errors, got: %+v", resp.Errors)
	}
}

func TestValidateGraph_BadCondition(t *testing.T) {
	bad := `{
      "StartAt": "a",
      "Nodes": {
        "a": {"Type": "end", "Config": {"Status": "success"}},
        "b": {"Type": "end", "Config": {"Status": "success"}}
      },
      "Edges": [{"From": "a", "To": "b", "Condition": "input.x === 1"}]
    }`
	resp := ValidateGraph([]byte(bad))
	if resp.Valid {
		t.Fatal("expected invalid for bad condition")
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
	joined := resp.Errors[0].Message
	if !strings.Contains(joined, "edge 0") {
		t.Fatalf("error message should reference edge 0, got: %q", joined)
	}
}

func TestValidateGraph_DanglingEdge(t *testing.T) {
	bad := `{
      "StartAt": "a",
      "Nodes": {"a": {"Type": "end", "Config": {"Status": "success"}}},
      "Edges": [{"From": "a", "To": "ghost"}]
    }`
	resp := ValidateGraph([]byte(bad))
	if resp.Valid {
		t.Fatal("expected invalid for dangling edge")
	}
	found := false
	for _, e := range resp.Errors {
		if e.Path == "edges[0].to" && strings.Contains(e.Message, "ghost") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected edges[0].to error about ghost, got: %+v", resp.Errors)
	}
}

func TestValidateGraph_MissingStart(t *testing.T) {
	bad := `{
      "StartAt": "missing",
      "Nodes": {"a": {"Type": "end", "Config": {"Status": "success"}}},
      "Edges": []
    }`
	resp := ValidateGraph([]byte(bad))
	if resp.Valid {
		t.Fatal("expected invalid for missing start node")
	}
	found := false
	for _, e := range resp.Errors {
		if e.Path == "StartAt" && strings.Contains(e.Message, "missing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected StartAt error about missing, got: %+v", resp.Errors)
	}
}
