package agent

import (
	"os"
	"path/filepath"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func TestBuildDefaultGraph_NoError(t *testing.T) {
	cfg := types.AgentConfig{
		MaxIterations: 50,
		Model:         "gpt-4",
	}
	g, err := BuildDefaultGraph(cfg)
	if err != nil {
		t.Fatalf("BuildDefaultGraph failed: %v", err)
	}
	if g.StartAt != "llm" {
		t.Fatalf("expected StartAt=llm, got %q", g.StartAt)
	}
	if _, ok := g.Nodes["llm"]; !ok {
		t.Fatal("missing llm node")
	}
	if _, ok := g.Nodes["route"]; !ok {
		t.Fatal("missing route node")
	}
	if _, ok := g.Nodes["end"]; !ok {
		t.Fatal("missing end node")
	}
	t.Logf("Graph parsed OK: StartAt=%s, nodes=%d, edges=%d",
		g.StartAt, len(g.Nodes), len(g.Edges))
}

func TestBuildGraph_FallsBackToDefault(t *testing.T) {
	cfg := types.AgentConfig{Model: "gpt-4"}
	g, err := BuildGraph(cfg)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}
	if g.StartAt != "llm" {
		t.Fatalf("expected default StartAt=llm, got %q", g.StartAt)
	}
}

func TestBuildGraph_LoadsCustomGraphFromPath(t *testing.T) {
	custom := `{"StartAt":"only","Nodes":{"only":{"Type":"end"}}}`
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := types.AgentConfig{Model: "gpt-4", GraphPath: path}
	g, err := BuildGraph(cfg)
	if err != nil {
		t.Fatalf("BuildGraph failed: %v", err)
	}
	if g.StartAt != "only" {
		t.Fatalf("expected custom StartAt=only, got %q", g.StartAt)
	}
}

func TestBuildGraph_ErrorsOnUnreadablePath(t *testing.T) {
	cfg := types.AgentConfig{GraphPath: filepath.Join(t.TempDir(), "does-not-exist.json")}
	if _, err := BuildGraph(cfg); err == nil {
		t.Fatal("expected error for missing graph file, got nil")
	}
}

func TestBuildGraph_RejectsInvalidConditionInCustomGraph(t *testing.T) {
	// A custom graph whose edge condition is not a valid expression must be
	// rejected at load time, not silently accepted.
	custom := `{"StartAt":"a","Nodes":{"a":{"Type":"end"},"b":{"Type":"end"}},"Edges":[{"From":"a","To":"b","Condition":"input.x + 1"}]}`
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := types.AgentConfig{GraphPath: path}
	if _, err := BuildGraph(cfg); err == nil {
		t.Fatal("expected error for invalid condition in custom graph, got nil")
	}
}

func TestNewAIAgent_FailsLoudOnBadCustomGraph(t *testing.T) {
	// An explicitly-requested custom graph that fails to load must surface as
	// an error, not silently fall back to the minimal graph.
	bad := `{"StartAt":"a","Nodes":{"a":{"Type":"end"}},"Edges":[{"From":"a","To":"a","Condition":"input.x + 1"}]}`
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewAIAgent(types.AgentConfig{GraphPath: path}, nil, nil); err == nil {
		t.Fatal("expected NewAIAgent to fail on invalid custom graph, got nil")
	}
}

func TestNewAIAgent_OKWithDefaultGraph(t *testing.T) {
	// No custom graph requested: construction must succeed with the default graph.
	if _, err := NewAIAgent(types.AgentConfig{}, nil, nil); err != nil {
		t.Fatalf("expected no error with default graph, got %v", err)
	}
}

func TestNewChildAgent_RunsSilently(t *testing.T) {
	// Child agents must not stream to the user terminal: their token output
	// would interleave with the parent's (and with sibling sub-agents under a
	// parallel dispatch). Only the top-level agent streams; child results flow
	// back as the delegate tool's return value.
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent.SetEventCallback(func(Event) {})

	child, err := parent.NewChildAgent("do something")
	if err != nil {
		t.Fatal(err)
	}
	if child.eventCB != nil {
		t.Fatal("expected child agent to run silently (no event callback), but one was set")
	}
}
