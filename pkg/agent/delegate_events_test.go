package agent

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchrunner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// TestParentToolEventsSurviveDelegation guards against a regression where a
// delegation (which re-wires the globally-registered ToolRunner to the child
// agent) leaves the main agent unable to emit "🔧 tool(...)" markers for the
// rest of the session. The child's OnToolStart routing must still reach the
// parent's event callback.
func TestParentToolEventsSurviveDelegation(t *testing.T) {
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var startEvents int
	parent.SetEventCallback(func(e Event) {
		if e.Type == EventToolStart {
			startEvents++
		}
	})

	// Creating a child re-wires the global ToolRunner.OnToolStart to the child.
	if _, err := parent.NewChildAgent("sub task"); err != nil {
		t.Fatal(err)
	}

	// Simulate a tool node firing OnToolStart during a later PARENT turn.
	entry, ok := orchestrator.LookupNodeType("tool")
	if !ok {
		t.Fatal("tool node type not registered")
	}
	r, ok := entry.Runner.(*orchrunner.ToolRunner)
	if !ok {
		t.Fatal("tool runner not found")
	}
	if r.OnToolStart != nil {
		r.OnToolStart(context.Background(), "palace_search", "{}")
	}

	if startEvents == 0 {
		t.Fatal("parent received no EventToolStart after delegation (global tool runner left stale/silent)")
	}
}
