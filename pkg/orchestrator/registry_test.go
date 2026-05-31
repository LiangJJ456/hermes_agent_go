package orchestrator

import (
	"context"
	"testing"
)

type stubRunner struct{}

func (stubRunner) Run(ctx context.Context, node *NodeSpec, input interface{},
	execCtx interface{}) (*NodeResult, error) {
	return nil, nil
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestListNodeTypes_SortedAndComplete(t *testing.T) {
	RegisterNodeType("zeta", stubRunner{}, nil)
	RegisterNodeType("alpha", stubRunner{}, nil)

	got := ListNodeTypes()

	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("ListNodeTypes not sorted: %v", got)
		}
	}
	if !containsStr(got, "alpha") || !containsStr(got, "zeta") {
		t.Fatalf("missing registered types: %v", got)
	}
}
