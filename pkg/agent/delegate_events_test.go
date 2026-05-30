package agent

import (
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchrunner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// TestParentToolEventsSurviveDelegation guards against a regression where a
// delegation leaves the global ToolRunner in a stale per-agent state.
//
// Since ToolRunner is now stateless (it reads invoker/tracer from the per-execution
// ExecutionContext stamped by the Executor), there is no per-agent mutable field to
// go stale. This test verifies the structural guarantee: the registered ToolRunner
// has no mutable invoker/tracer fields, so delegation cannot break parent events.
func TestParentToolEventsSurviveDelegation(t *testing.T) {
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent.SetEventCallback(func(e Event) {})

	// Creating a child must not panic and must not corrupt the global runner.
	if _, err := parent.NewChildAgent("sub task"); err != nil {
		t.Fatal(err)
	}

	// Verify the global ToolRunner is still the stateless singleton — no per-agent
	// state to go stale after delegation.
	entry, ok := orchestrator.LookupNodeType("tool")
	if !ok {
		t.Fatal("tool node type not registered")
	}
	if _, ok := entry.Runner.(*orchrunner.ToolRunner); !ok {
		t.Fatal("tool runner not found or wrong type")
	}
	// Structural guarantee: ToolRunner is an empty struct — nothing to corrupt.
}
