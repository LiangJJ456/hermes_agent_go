package context

import (
	"code.byted.org/ad_creative/hermes_agent_go/pkg/trace"
)

// ExecutionContext holds state for a single Graph execution.
type ExecutionContext struct {
	WorkMem        *WorkingMemory
	ConvMem        *ConversationMemory
	TraceID        string
	CurrentSpanID  string
	DefinitionName string

	// Per-execution services, stamped by the running Executor. Runners read
	// these instead of holding their own (formerly global, racy) copies.
	LLMInvoker  LLMInvoker
	ToolInvoker ToolInvoker
	Executor    GraphExecutor
	Tracer      trace.Tracer
}

// WorkingMemory is the mutable state during Graph execution.
type WorkingMemory struct {
	Input      interface{}
	State      map[string]interface{}
	Scratchpad []string
	LastResult interface{}
}

// NewWorkingMemory creates working memory initialized with input.
// LastResult is also set to input so the first node in the graph receives it.
func NewWorkingMemory(input interface{}) *WorkingMemory {
	return &WorkingMemory{
		Input:      input,
		State:      make(map[string]interface{}),
		Scratchpad: make([]string, 0),
		LastResult: input,
	}
}

// AppendScratchpad adds a thought to the scratchpad.
func (wm *WorkingMemory) AppendScratchpad(thought string) {
	wm.Scratchpad = append(wm.Scratchpad, thought)
}

// NewExecutionContext creates an execution context from input.
func NewExecutionContext(input interface{}) *ExecutionContext {
	return &ExecutionContext{
		WorkMem: NewWorkingMemory(input),
	}
}

// Fork creates a child ExecutionContext for parallel/map branch isolation.
func (ec *ExecutionContext) Fork() *ExecutionContext {
	stateCopy := make(map[string]interface{})
	for k, v := range ec.WorkMem.State {
		stateCopy[k] = v
	}
	child := &ExecutionContext{
		WorkMem: &WorkingMemory{
			Input:      ec.WorkMem.Input,
			State:      stateCopy,
			Scratchpad: make([]string, 0),
		},
		ConvMem:        ec.ConvMem,
		TraceID:        ec.TraceID,
		DefinitionName: ec.DefinitionName,
	}
	return child
}

// Merge merges a child ExecutionContext's State back into this one.
func (ec *ExecutionContext) Merge(child *ExecutionContext) {
	for k, v := range child.WorkMem.State {
		ec.WorkMem.State[k] = v
	}
}
