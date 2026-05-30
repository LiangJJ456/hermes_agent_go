package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// StreamDeltaFunc is a callback for streaming LLM output deltas.
type StreamDeltaFunc func(ctx context.Context, content string)

// LLMRunner executes an LLM node by delegating to an LLMInvoker.
type LLMRunner struct {
	Invoker       LLMInvoker
	OnStreamDelta StreamDeltaFunc // optional: called for each streaming delta
}

// SetInvoker sets the LLM invoker (called after construction).
func (r *LLMRunner) SetInvoker(inv LLMInvoker) {
	r.Invoker = inv
}

func (r *LLMRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg LLMConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*LLMConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Invoker == nil {
		return nil, fmt.Errorf("llm runner: no invoker configured")
	}

	// Build messages: prefer full conversation history from ConvMem
	var messages []LLMMessage

	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.ConvMem != nil && len(ec.ConvMem.Messages) > 0 {
		// Use full conversation history (system + all user/assistant turns)
		for _, m := range ec.ConvMem.Messages {
			messages = append(messages, LLMMessage{
				Role:       m.Role,
				Content:    m.Content,
				Name:       m.Name,
				ToolCalls:  m.ToolCalls,
				ToolCallID: m.ToolCallID,
			})
		}
	} else {
		// Fallback: single-turn (backward compatible)
		if cfg.SystemPrompt != "" {
			messages = append(messages, LLMMessage{Role: "system", Content: cfg.SystemPrompt})
		}
		userContent := formatInput(input)
		messages = append(messages, LLMMessage{Role: "user", Content: userContent})
	}

	// Use streaming if OnStreamDelta callback is set
	var result *orchestrator.NodeResult
	var callErr error
	if r.OnStreamDelta != nil {
		result, callErr = r.Invoker.ChatStream(ctx, cfg.Model, messages, cfg.Tools, cfg, func(delta string) {
			r.OnStreamDelta(ctx, delta)
		})
	} else {
		result, callErr = r.Invoker.Chat(ctx, cfg.Model, messages, cfg.Tools, cfg)
	}
	if callErr != nil {
		return nil, callErr
	}

	// Append assistant message (with tool_calls) to ConvMem so the next LLM
	// call in the graph loop sees the full history including this response.
	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.ConvMem != nil && result != nil {
		if outMap, ok := result.Output.(map[string]interface{}); ok {
			asstMsg := agcontext.Message{
				Role:    "assistant",
				Content: "",
			}
			if c, ok := outMap["content"].(string); ok {
				asstMsg.Content = c
			}
			if tcs, ok := outMap["tool_calls"].([]map[string]interface{}); ok {
				asstMsg.ToolCalls = tcs
			}
			ec.ConvMem.AddMessage(asstMsg)
		}
	}

	return result, nil
}

func formatInput(input interface{}) string {
	if input == nil {
		return ""
	}
	if s, ok := input.(string); ok {
		return s
	}
	b, _ := json.Marshal(input)
	return string(b)
}

func init() {
	orchestrator.RegisterNodeType("llm", &LLMRunner{}, &LLMConfig{})
}
