package adapters

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// RouterAdapter adapts hermes model.Router to runner.LLMInvoker.
type RouterAdapter struct {
	Router     *model.Router
	Registry   *registry.Registry
	Config     types.AgentConfig
	MemoryMgr  *memory.Manager
	Compressor CompressorInterface
}

func (a *RouterAdapter) Chat(ctx context.Context, modelName string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig) (*orchestrator.NodeResult, error) {

	return a.call(ctx, modelName, messages, tools, cfg, nil)
}

func (a *RouterAdapter) ChatStream(ctx context.Context, modelName string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error) {

	return a.call(ctx, modelName, messages, tools, cfg, onDelta)
}

func (a *RouterAdapter) call(ctx context.Context, modelName string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error) {

	provider, resolvedModel, err := a.Router.Resolve(modelName)
	if err != nil {
		return nil, err
	}

	// Build tool schemas
	toolSchemas := a.Registry.GetSchemas(a.Config.EnabledToolsets, a.Config.DisabledTools)
	if a.MemoryMgr != nil {
		for _, ms := range a.MemoryMgr.GetAllToolSchemas() {
			paramsJSON, _ := json.Marshal(ms.Parameters)
			toolSchemas = append(toolSchemas, types.ToolSchema{
				Type: "function",
				Function: types.FunctionSchema{
					Name:        ms.Name,
					Description: ms.Description,
					Parameters:  paramsJSON,
				},
			})
		}
	}

	// Convert runner.LLMMessage to types.Message
	chatMsgs := make([]types.Message, 0, len(messages))
	for _, m := range messages {
		msg := types.Message{
			Role:       types.Role(m.Role),
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		// Convert []map[string]interface{} tool_calls to []types.ToolCall
		for _, tc := range m.ToolCalls {
			toolCall := types.ToolCall{}
			if id, ok := tc["id"].(string); ok {
				toolCall.ID = id
			}
			if tp, ok := tc["type"].(string); ok {
				toolCall.Type = tp
			}
			if fn, ok := tc["function"].(map[string]interface{}); ok {
				if name, ok := fn["name"].(string); ok {
					toolCall.Function.Name = name
				}
				if args, ok := fn["arguments"].(string); ok {
					toolCall.Function.Arguments = args
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, toolCall)
		}
		chatMsgs = append(chatMsgs, msg)
	}

	temp := cfg.Temperature
	if temp <= 0 {
		temp = a.Config.Temperature
	}
	maxTok := cfg.MaxTokens
	if maxTok <= 0 {
		maxTok = a.Config.MaxTokens
	}

	req := &model.ChatRequest{
		Model:       resolvedModel,
		Messages:    chatMsgs,
		Tools:       toolSchemas,
		Temperature: temp,
		MaxTokens:   maxTok,
	}

	var resp *types.ChatResponse
	if onDelta != nil {
		resp, err = provider.ChatStream(ctx, req, func(delta types.StreamDelta) {
			if delta.Content != "" {
				onDelta(delta.Content)
			}
		})
	} else {
		resp, err = provider.Chat(ctx, req)
	}
	if err != nil {
		return nil, err
	}

	hasToolCalls := len(resp.Message.ToolCalls) > 0
	needsCompression := false
	if a.Compressor != nil {
		needsCompression = a.Compressor.NeedsCompression(chatMsgs)
	}
	output := map[string]interface{}{
		"content":           resp.Message.Content,
		"has_tool_calls":    hasToolCalls,
		"needs_compression": needsCompression,
		"finish_reason":     string(resp.Message.FinishReason),
	}

	// Convert tool calls for the orchestrator
	if hasToolCalls {
		tcMaps := make([]map[string]interface{}, len(resp.Message.ToolCalls))
		for i, tc := range resp.Message.ToolCalls {
			tcMaps[i] = map[string]interface{}{
				"id":   tc.ID,
				"type": tc.Type,
				"function": map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			}
		}
		output["tool_calls"] = tcMaps
	}

	return &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: output,
	}, nil
}
