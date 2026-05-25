package agent

import (
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
