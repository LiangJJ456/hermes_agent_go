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
