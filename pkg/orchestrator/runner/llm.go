package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// LLMRunner executes an LLM node by delegating to an LLMInvoker.
type LLMRunner struct{}

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

	ec, _ := execCtx.(*agcontext.ExecutionContext)
	if ec == nil || ec.LLMInvoker == nil {
		return nil, fmt.Errorf("llm runner: no invoker configured")
	}

	// Build messages: prefer full conversation history from ConvMem
	var messages []LLMMessage
	if ec.ConvMem != nil && len(ec.ConvMem.Messages) > 0 {
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
		if cfg.SystemPrompt != "" {
			messages = append(messages, LLMMessage{Role: "system", Content: cfg.SystemPrompt})
		}
		messages = append(messages, LLMMessage{Role: "user", Content: formatInput(input)})
	}

	// Stream when a tracer is present so deltas reach the display.
	var result *orchestrator.NodeResult
	var callErr error
	if ec.Tracer != nil {
		result, callErr = ec.LLMInvoker.ChatStream(ctx, cfg.Model, messages, cfg.Tools, cfg, func(delta string) {
			ec.Tracer.OnStreamDelta(ctx, delta)
		})
	} else {
		result, callErr = ec.LLMInvoker.Chat(ctx, cfg.Model, messages, cfg.Tools, cfg)
	}
	if callErr != nil {
		return nil, callErr
	}

	// Append assistant message to ConvMem (unchanged logic).
	if ec.ConvMem != nil && result != nil {
		if outMap, ok := result.Output.(map[string]interface{}); ok {
			asstMsg := agcontext.Message{Role: "assistant"}
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
