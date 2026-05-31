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

// ToolRunner executes a tool by delegating to a ToolInvoker.
type ToolRunner struct{}

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

	ec, _ := execCtx.(*agcontext.ExecutionContext)
	if ec == nil || ec.ToolInvoker == nil {
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
	if ec.Tracer != nil {
		ec.Tracer.OnToolStart(ctx, resource, toolArgsStr)
	}

	result, err := ec.ToolInvoker.Invoke(ctx, resource, input, cfg.Timeout)
	if err != nil {
		return nil, err
	}

	// Append tool result to ConvMem only for LLM-initiated tool calls (those with a
	// tool_call_id). Graph-level tool nodes (compress, wait_and_retry, etc.) have no
	// tool_call_id and must NOT add a role:"tool" message — the API rejects tool
	// messages that lack a matching assistant tool_calls entry.
	if ec.ConvMem != nil {
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
