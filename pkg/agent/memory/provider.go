package memory

import "context"

// Provider 记忆 Provider 抽象接口
// 内建 Provider（name="builtin"）始终存在且不可移除
// 外部 Provider 至多 1 个（防 Schema 膨胀 + 后端冲突）
type Provider interface {
	// Name 供应商标识，如 "builtin", "honcho", "mem0"
	Name() string

	// IsAvailable 是否已配置好可用（不做网络调用，仅检查配置和依赖）
	IsAvailable() bool

	// ── 核心生命周期 ──

	// Initialize 初始化，每个 session 调用一次
	Initialize(ctx context.Context, opts InitOpts) error

	// SystemPromptBlock 返回注入 system prompt 的静态文本（空串=跳过）
	SystemPromptBlock() string

	// Prefetch 在每次 LLM 调用前召回相关记忆上下文
	Prefetch(ctx context.Context, query string, sessionID string) string

	// QueuePrefetch 异步预取下一轮记忆（后台执行）
	QueuePrefetch(query string, sessionID string)

	// SyncTurn 每轮结束后持久化对话到后端（应非阻塞）
	SyncTurn(userContent, assistantContent, sessionID string)

	// GetToolSchemas 返回此 Provider 暴露的工具 Schema（空=仅上下文）
	GetToolSchemas() []ToolSchemaDef

	// HandleToolCall 处理工具调用，返回 JSON 结果
	HandleToolCall(ctx context.Context, toolName string, args map[string]any) (string, error)

	// Shutdown 清理退出
	Shutdown()

	// ── 可选 Hook（默认空实现） ──

	// OnTurnStart 每轮开始时通知
	OnTurnStart(turnNumber int, message string, meta map[string]any)

	// OnSessionEnd 会话结束时通知（显式退出/超时，非每轮）
	OnSessionEnd(messages []map[string]any)

	// OnPreCompress 上下文压缩前，从即将丢弃的消息中提取洞察
	OnPreCompress(messages []map[string]any) string

	// OnMemoryWrite 内建记忆写入时通知外部 Provider 镜像
	OnMemoryWrite(action, target, content string)

	// OnDelegation 子 Agent 完成时通知父 Agent 的 Provider
	OnDelegation(task, result, childSessionID string)
}

// InitOpts 初始化选项
type InitOpts struct {
	SessionID       string
	HermesHome      string // 配置根目录
	Platform        string // "cli", "telegram", etc.
	AgentContext     string // "primary", "subagent", "cron"
	AgentIdentity    string // profile name
	ParentSessionID  string
	UserID           string
}

// ToolSchemaDef 工具 Schema 定义（轻量版，不直接依赖 types 包）
type ToolSchemaDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// BaseProvider 默认空实现，外部 Provider 可内嵌复用
type BaseProvider struct{}

func (BaseProvider) SystemPromptBlock() string                              { return "" }
func (BaseProvider) Prefetch(_ context.Context, _, _ string) string         { return "" }
func (BaseProvider) QueuePrefetch(_, _ string)                              {}
func (BaseProvider) SyncTurn(_, _, _ string)                                {}
func (BaseProvider) GetToolSchemas() []ToolSchemaDef                        { return nil }
func (BaseProvider) HandleToolCall(_ context.Context, _ string, _ map[string]any) (string, error) {
	return `{"error":"not implemented"}`, nil
}
func (BaseProvider) Shutdown()                                         {}
func (BaseProvider) OnTurnStart(_ int, _ string, _ map[string]any)     {}
func (BaseProvider) OnSessionEnd(_ []map[string]any)                   {}
func (BaseProvider) OnPreCompress(_ []map[string]any) string           { return "" }
func (BaseProvider) OnMemoryWrite(_, _, _ string)                      {}
func (BaseProvider) OnDelegation(_, _, _ string)                       {}
