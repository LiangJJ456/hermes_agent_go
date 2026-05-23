package executor

import (
	"context"
	"fmt"
	"sort"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// Route determines the next node based on the current node's result and graph edges.
// Priority: result.Next (dynamic) > edge with highest Priority > error.
func Route(ctx context.Context, currentNode string, result *orchestrator.NodeResult,
	edges []orchestrator.EdgeSpec, ec interface{}) (string, error) {

	// Dynamic override from the node result
	if result.Next != "" {
		return result.Next, nil
	}

	// Find outgoing edges from currentNode, sorted by priority (lower = higher priority)
	var outgoing []orchestrator.EdgeSpec
	for _, e := range edges {
		if e.From == currentNode {
			outgoing = append(outgoing, e)
		}
	}

	sort.Slice(outgoing, func(i, j int) bool {
		return outgoing[i].Priority < outgoing[j].Priority
	})

	if len(outgoing) > 0 {
		return outgoing[0].To, nil
	}

	return "", fmt.Errorf("no edge from node %q and no dynamic Next set", currentNode)
}
