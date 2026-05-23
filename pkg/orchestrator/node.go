package orchestrator

import "context"

// NodeRunner is implemented by every node type.
// The execCtx parameter carries execution state; cast to *context.ExecutionContext as needed.
type NodeRunner interface {
	Run(ctx context.Context, node *NodeSpec, input interface{},
		execCtx interface{}) (*NodeResult, error)
}

// NodeResult is the output of a node execution.
type NodeResult struct {
	Status    string      // "continue" | "end" | "pending"
	Output    interface{}
	Next      string      // dynamic next node (optional, for choice nodes)
	Error     string
	Cause     string
	Interrupt bool        // true means pause and wait for external input
}
