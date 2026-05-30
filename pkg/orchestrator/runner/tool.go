package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/trace"
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

// ToolStartFunc is fired right before a tool starts executing, so long-running
// tools can give the user real-time feedback (e.g. a "🔧 toolname(...)" line).
type ToolStartFunc func(ctx context.Context, toolName, toolArgs string)

// ToolRunner executes a tool by delegating to a ToolInvoker.
type ToolRunner struct {
	Invoker     ToolInvoker
	OnToolStart ToolStartFunc // optional: fired before Invoker.Invoke
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

	// Strip .waitForCallback suffix for actual invocation
	resource := cfg.Resource
	if strings.HasSuffix(resource, ".waitForCallback") {
		resource = strings.TrimSuffix(resource, ".waitForCallback")
	}

	// Write tool info to current span for tracing/event callbacks
	var toolArgsStr string
	if input != nil {
		if b, merr := json.Marshal(input); merr == nil {
			toolArgsStr = string(b)
		}
	}
	if span := trace.SpanFromContext(ctx); span != nil {
		span.SetAttribute("tool_name", resource)
		if toolArgsStr != "" {
			span.SetAttribute("tool_args", toolArgsStr)
		}
	}
	// Fire start event BEFORE invoke so long tools give live feedback.
	if r.OnToolStart != nil {
		r.OnToolStart(ctx, resource, toolArgsStr)
	}

	result, err := r.Invoker.Invoke(ctx, resource, input, cfg.Timeout)
	if err != nil {
		return nil, err
	}

	// Append tool result to ConvMem only for LLM-initiated tool calls (those with a
	// tool_call_id). Graph-level tool nodes (compress, wait_and_retry, etc.) have no
	// tool_call_id and must NOT add a role:"tool" message — the API rejects tool
	// messages that lack a matching assistant tool_calls entry.
	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.ConvMem != nil {
		toolCallID := ""
		if inputMap, ok := input.(map[string]interface{}); ok {
			if id, ok := inputMap["tool_call_id"].(string); ok {
				toolCallID = id
			}
		}
		if toolCallID != "" {
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
				Name:       resource,
				ToolCallID: toolCallID,
			})
		}
	}

	// Async mode: explicit Async config OR resource suffix .waitForCallback
	isAsync := cfg.Async || strings.HasSuffix(cfg.Resource, ".waitForCallback")
	if isAsync && result.Status != orchestrator.StatusEnd {
		result.Status = orchestrator.StatusPending
		result.Interrupt = true
	}

	return result, nil
}

func init() {
	orchestrator.RegisterNodeType("tool", &ToolRunner{}, &ToolConfig{})
}
