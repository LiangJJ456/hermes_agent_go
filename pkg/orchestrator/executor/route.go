package executor

import (
	"context"
	"fmt"
	"sort"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/condition"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// Route determines the next node from the current node's result and graph edges.
// Priority: result.Next (dynamic) > first outgoing edge (in Priority order) whose
// Condition passes > error. An edge with no Condition is an unconditional fallback
// and always passes.
func Route(ctx context.Context, currentNode string, result *orchestrator.NodeResult,
	edges []orchestrator.EdgeSpec, ec interface{}) (string, error) {

	// Dynamic override from the node result.
	if result.Next != "" {
		return result.Next, nil
	}

	// Outgoing edges sorted by priority (lower = higher priority).
	var outgoing []orchestrator.EdgeSpec
	for _, e := range edges {
		if e.From == currentNode {
			outgoing = append(outgoing, e)
		}
	}
	sort.SliceStable(outgoing, func(i, j int) bool {
		return outgoing[i].Priority < outgoing[j].Priority
	})

	scope := condition.Scope{Input: result.Output}
	if ac, ok := ec.(*agcontext.ExecutionContext); ok && ac.WorkMem != nil {
		scope.State = ac.WorkMem.State
	}

	for _, e := range outgoing {
		if e.Condition == "" {
			return e.To, nil
		}
		matched, err := condition.Evaluate(e.Condition, scope)
		if err != nil {
			return "", fmt.Errorf("edge %q->%q condition %q: %w", currentNode, e.To, e.Condition, err)
		}
		if matched {
			return e.To, nil
		}
	}

	return "", fmt.Errorf("no matching edge from node %q (no condition passed and no fallback)", currentNode)
}
