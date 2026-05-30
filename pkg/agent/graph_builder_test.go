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

func TestNewChildAgent_DropsStreamForwardsOtherEvents(t *testing.T) {
	// Child agents must not stream tokens to the terminal (those interleave with
	// the parent's and siblings' output), but other events — notably tool
	// start/end — must still forward to the parent's callback so the main
	// agent's "🔧 tool(...)" markers keep working after a delegation.
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var forwarded []Event
	parent.SetEventCallback(func(e Event) { forwarded = append(forwarded, e) })

	child, err := parent.NewChildAgent("do something")
	if err != nil {
		t.Fatal(err)
	}
	if child.eventCB == nil {
		t.Fatal("expected child to have a forwarding callback")
	}

	child.eventCB(Event{Type: EventStreamDelta, Content: "tok"})
	child.eventCB(Event{Type: EventToolStart, ToolName: "t"})

	var toolStart *Event
	for i := range forwarded {
		if forwarded[i].Type == EventStreamDelta {
			t.Fatal("child stream deltas must not be forwarded (would interleave)")
		}
		if forwarded[i].Type == EventToolStart {
			toolStart = &forwarded[i]
		}
	}
	if toolStart == nil {
		t.Fatal("child tool-start events must forward to the parent callback")
	}
	if !toolStart.FromSubAgent {
		t.Fatal("forwarded sub-agent events must be marked FromSubAgent for display labeling")
	}
}
