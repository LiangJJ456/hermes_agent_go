package orchestrator

import "context"

// NodeRunner is implemented by every node type.
// The execCtx parameter carries execution state; cast to *context.ExecutionContext as needed.
type NodeRunner interface {
	Run(ctx context.Context, node *NodeSpec, input interface{},
		execCtx interface{}) (*NodeResult, error)
}

// Status constants for NodeResult.
const (
	StatusContinue = "continue"
	StatusEnd      = "end"
	StatusPending  = "pending"
)

// NodeResult is the output of a node execution.
type NodeResult struct {
	Status    string // use StatusContinue / StatusEnd / StatusPending
	Output    interface{}
	Next      string // dynamic next node (optional, for choice nodes)
	Error     string
	Cause     string
	Interrupt bool // true means pause and wait for external input
}

// ExecutionSnapshot captures execution state at an interrupt point.
// Full lifecycle management is in pkg/orchestrator/executor.
type ExecutionSnapshot struct {
	ExecutionID string
	CurrentNode string
	Step        int
	WorkMem     interface{} // *context.WorkingMemory
	ConvMem     interface{} // *context.ConversationMemory
	GraphHash   string
	CreatedAt   interface{} // time.Time
}
