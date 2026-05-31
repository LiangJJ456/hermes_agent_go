package agent

import (
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/adapters"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// adapterDisablesDelegate reports whether a RouterAdapter's config filters out
// the delegate tools.
func adapterDisablesDelegate(inv interface{}) bool {
	ra, ok := inv.(*adapters.RouterAdapter)
	if !ok {
		return false
	}
	for _, d := range ra.Config.DisabledTools {
		if d == "delegate_task_async" {
			return true
		}
	}
	return false
}

// TestDelegationKeepsParentInvoker verifies that creating a child agent (which
// disables delegate tools for itself) does NOT affect the parent's own invoker.
// Previously a shared global runner leaked the child's restricted invoker to the
// parent, so async delegation only worked on the first request. With per-agent
// executors there is no shared mutable state, so each agent keeps its own invoker.
func TestDelegationKeepsParentInvoker(t *testing.T) {
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if adapterDisablesDelegate(parent.llmInvoker) {
		t.Fatal("parent invoker should offer delegate tools")
	}

	child, err := parent.NewChildAgent("sub task")
	if err != nil {
		t.Fatal(err)
	}
	if !adapterDisablesDelegate(child.llmInvoker) {
		t.Fatal("child invoker should disable delegate tools")
	}
	// The parent's invoker must be untouched by the delegation.
	if adapterDisablesDelegate(parent.llmInvoker) {
		t.Fatal("parent invoker was mutated by delegation (state leak)")
	}
}
