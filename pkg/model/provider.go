package model

import (
	"context"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// StreamCallback 流式回调
type StreamCallback func(delta types.StreamDelta)

// Provider LLM 供应商抽象接口
type Provider interface {
	// Name 供应商标识
	Name() string

	// Chat 同步调用（阻塞直到完整响应）
	Chat(ctx context.Context, req *ChatRequest) (*types.ChatResponse, error)

	// ChatStream 流式调用
	ChatStream(ctx context.Context, req *ChatRequest, cb StreamCallback) (*types.ChatResponse, error)

	// SupportsTools 是否支持工具调用
	SupportsTools() bool

	// MaxContextTokens 最大上下文长度
	MaxContextTokens() int
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model       string             `json:"model"`
	Messages    []types.Message    `json:"messages"`
	Tools       []types.ToolSchema `json:"tools,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens,omitempty"`
	TopP        float64            `json:"top_p,omitempty"`
	Stop        []string           `json:"stop,omitempty"`
}
