package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// ToolConfig configures a tool node.
type ToolConfig struct {
	Resource   string                 `json:"Resource"`
	Parameters map[string]interface{} `json:"Parameters,omitempty"`
	Timeout    uint                   `json:"Timeout,omitempty"`
	Async      bool                   `json:"Async,omitempty"`
}

// ToolInvoker abstracts tool execution. hermes adapts tool.Registry to this.
type ToolInvoker interface {
	Invoke(ctx context.Context, resource string, input interface{},
		timeout uint) (*orchestrator.NodeResult, error)
}

// ToolRunner executes a tool by delegating to a ToolInvoker.
type ToolRunner struct {
	Invoker ToolInvoker
}

// SetInvoker sets the tool invoker.
func (r *ToolRunner) SetInvoker(inv ToolInvoker) {
	r.Invoker = inv
}

func (r *ToolRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg ToolConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ToolConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Invoker == nil {
		return nil, fmt.Errorf("tool runner: no invoker configured")
	}

	result, err := r.Invoker.Invoke(ctx, cfg.Resource, input, cfg.Timeout)
	if err != nil {
		return nil, err
	}

	// Append tool result to ConvMem so the LLM sees tool output on the next call.
	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.ConvMem != nil {
		toolCallID := ""
		if inputMap, ok := input.(map[string]interface{}); ok {
			if id, ok := inputMap["tool_call_id"].(string); ok {
				toolCallID = id
			}
		}
		resultContent := ""
		if result.Output != nil {
			if s, ok := result.Output.(string); ok {
				resultContent = s
			} else {
				b, _ := json.Marshal(result.Output)
				resultContent = string(b)
			}
		}
		ec.ConvMem.AddMessage(agcontext.Message{
			Role:       "tool",
			Content:    resultContent,
			Name:       cfg.Resource,
			ToolCallID: toolCallID,
		})
	}

	if cfg.Async && result.Status != orchestrator.StatusEnd {
		result.Status = orchestrator.StatusPending
		result.Interrupt = true
	}

	return result, nil
}

func init() {
	orchestrator.RegisterNodeType("tool", &ToolRunner{}, &ToolConfig{})
}
