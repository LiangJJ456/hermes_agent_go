package context

import (
	stdctx "context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// LLMConfig configures an llm node. (Moved from runner so it can appear in the
// LLMInvoker signature without an import cycle; runner re-exports it as an alias.)
type LLMConfig struct {
	Model        string          `json:"Model"`
	SystemPrompt string          `json:"SystemPrompt,omitempty"`
	UserPrompt   string          `json:"UserPrompt,omitempty"`
	Tools        []string        `json:"Tools,omitempty"`
	OutputSchema json.RawMessage `json:"OutputSchema,omitempty"`
	Temperature  float64         `json:"Temperature,omitempty"`
	MaxTokens    int             `json:"MaxTokens,omitempty"`
}

// LLMMessage is a single message sent to the LLM.
type LLMMessage struct {
	Role       string                   `json:"Role"`
	Content    string                   `json:"Content"`
	Name       string                   `json:"Name,omitempty"`
	ToolCalls  []map[string]interface{} `json:"ToolCalls,omitempty"`
	ToolCallID string                   `json:"ToolCallID,omitempty"`
}

// LLMInvoker abstracts the actual LLM call.
type LLMInvoker interface {
	Chat(ctx stdctx.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig) (*orchestrator.NodeResult, error)
	ChatStream(ctx stdctx.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error)
}

// ToolInvoker abstracts tool execution.
type ToolInvoker interface {
	Invoke(ctx stdctx.Context, resource string, input interface{},
		timeout uint) (*orchestrator.NodeResult, error)
}

// GraphExecutor executes a sub-graph. The orchestrator Executor implements this.
type GraphExecutor interface {
	Execute(ctx stdctx.Context, g *orchestrator.Graph,
		input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error)
}

// ContextGraphExecutor extends GraphExecutor with ConvMem-aware execution.
type ContextGraphExecutor interface {
	GraphExecutor
	ExecuteWithContext(ctx stdctx.Context, g *orchestrator.Graph,
		ec *ExecutionContext) (interface{}, *orchestrator.ExecutionSnapshot, error)
}
