package agent

import (
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchrunner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// TestToolRunnerIsStateless verifies that the registered ToolRunner is the
// stateless empty-struct singleton: it has no per-agent mutable fields
// (invoker, tracer, etc.), so creating a child agent via delegation cannot
// corrupt any global runner state.
// The actual child→parent event forwarding behaviour is covered by
// TestNewChildAgent_DropsStreamForwardsOtherEvents.
func TestToolRunnerIsStateless(t *testing.T) {
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
