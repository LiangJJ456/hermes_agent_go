package types

import "time"

// Role 消息角色
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleDeveloper Role = "developer" // GPT-5/Codex 使用
)

// FinishReason 模型停止原因
type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonToolCalls FinishReason = "tool_calls"
	FinishReasonLength    FinishReason = "length"
)

// ToolCall 工具调用
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

// Message 对话消息
type Message struct {
	Role             Role         `json:"role"`
	Content          string       `json:"content,omitempty"`
	Name             string       `json:"name,omitempty"`
	ToolCalls        []ToolCall   `json:"tool_calls,omitempty"`
	ToolCallID       string       `json:"tool_call_id,omitempty"`
	Reasoning        string       `json:"reasoning,omitempty"`
	FinishReason     FinishReason `json:"finish_reason,omitempty"`
	Timestamp        time.Time    `json:"timestamp,omitempty"`
	TokenCount       int          `json:"token_count,omitempty"`
	ReasoningDetails string       `json:"reasoning_details,omitempty"`
}

// Usage Token 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}

// ChatResponse LLM 响应
type ChatResponse struct {
	Message Message `json:"message"`
	Usage   Usage   `json:"usage"`
}

// StreamDelta 流式增量
type StreamDelta struct {
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`

	// tool_call 实时通知（streaming 时发出）
	ToolCallStart bool   `json:"tool_call_start,omitempty"` // 新 tool_call 开始
	ToolCallName  string `json:"tool_call_name,omitempty"`  // 工具名称
	ToolCallID    string `json:"tool_call_id,omitempty"`    // tool_call ID
}
