package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// BuiltinProvider 内建记忆 Provider（始终存在，不可移除）
// 基于 MEMORY.md + USER.md 文件存储
type BuiltinProvider struct {
	store *Store
}

// NewBuiltinProvider 创建内建 Provider
func NewBuiltinProvider(store *Store) *BuiltinProvider {
	return &BuiltinProvider{store: store}
}

func (p *BuiltinProvider) Name() string { return "builtin" }

func (p *BuiltinProvider) IsAvailable() bool { return true } // 始终可用

func (p *BuiltinProvider) Initialize(_ context.Context, opts InitOpts) error {
	log.Info("builtin memory initializing", "session_id", opts.SessionID, "dir", opts.HermesHome)
	return p.store.LoadFromDisk()
}

func (p *BuiltinProvider) SystemPromptBlock() string {
	var parts []string

	memBlock := p.store.Snapshot("memory")
	if memBlock != "" {
		parts = append(parts, memBlock)
	}

	userBlock := p.store.Snapshot("user")
	if userBlock != "" {
		parts = append(parts, userBlock)
	}

	if len(parts) == 0 {
		return ""
	}

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += "\n\n"
		}
		result += part
	}
	return result
}

// Prefetch — builtin 记忆不参与每轮预取,仅经 SystemPromptBlock 注入 system prompt。
func (p *BuiltinProvider) Prefetch(_ context.Context, _ string, _ string) string {
	return ""
}

// QueuePrefetch — builtin 已退出预取,no-op。
func (p *BuiltinProvider) QueuePrefetch(_ string, _ string) {}

// SyncTurn 每轮结束后持久化 —— 内建 Provider 的写入在 tool call 时已实时落盘，
// 因此此处为 no-op
func (p *BuiltinProvider) SyncTurn(_, _, _ string) {}

// GetToolSchemas 返回 memory 工具的 Schema
func (p *BuiltinProvider) GetToolSchemas() []ToolSchemaDef {
	return []ToolSchemaDef{
		{
			Name: "memory",
			Description: "Save durable information to persistent memory that survives across sessions. " +
				"Memory is injected into future turns, so keep it compact and focused on facts " +
				"that will still matter later.\n\n" +
				"WHEN TO SAVE (do this proactively, don't wait to be asked):\n" +
				"- User corrects you or says 'remember this' / 'don't do that again'\n" +
				"- User shares a preference, habit, or personal detail\n" +
				"- You discover something about the environment (OS, tools, project structure)\n" +
				"- You learn a convention, API quirk, or workflow specific to this user's setup\n\n" +
				"TWO TARGETS:\n" +
				"- 'user': who the user is -- name, role, preferences, communication style\n" +
				"- 'memory': your notes -- environment facts, project conventions, lessons learned\n\n" +
				"ACTIONS: add (new entry), replace (update -- old_text identifies it), " +
				"remove (delete -- old_text identifies it), read (view current state).\n\n" +
				"Do NOT save task progress, session outcomes, or temporary state.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"add", "replace", "remove", "read"},
						"description": "The action to perform.",
					},
					"target": map[string]any{
						"type":        "string",
						"enum":        []string{"memory", "user"},
						"description": "Which memory store: 'memory' for personal notes, 'user' for user profile.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The entry content. Required for 'add' and 'replace'.",
					},
					"old_text": map[string]any{
						"type":        "string",
						"description": "Short unique substring identifying the entry to replace or remove.",
					},
				},
				"required": []string{"action", "target"},
			},
		},
	}
}

// HandleToolCall 处理 memory 工具调用
func (p *BuiltinProvider) HandleToolCall(_ context.Context, toolName string, args map[string]any) (string, error) {
	if toolName != "memory" {
		return "", fmt.Errorf("builtin provider does not handle tool '%s'", toolName)
	}

	action, _ := args["action"].(string)
	target, _ := args["target"].(string)
	content, _ := args["content"].(string)
	oldText, _ := args["old_text"].(string)

	if target == "" {
		target = "memory"
	}
	if target != "memory" && target != "user" {
		return errJSON("Invalid target '%s'. Use 'memory' or 'user'.", target), nil
	}

	var result Result

	switch action {
	case "add":
		if content == "" {
			return errJSON("Content is required for 'add' action."), nil
		}
		result = p.store.Add(target, content)

	case "replace":
		if oldText == "" {
			return errJSON("old_text is required for 'replace' action."), nil
		}
		if content == "" {
			return errJSON("content is required for 'replace' action."), nil
		}
		result = p.store.Replace(target, oldText, content)

	case "remove":
		if oldText == "" {
			return errJSON("old_text is required for 'remove' action."), nil
		}
		result = p.store.Remove(target, oldText)

	case "read":
		result = p.store.Read(target)

	default:
		return errJSON("Unknown action '%s'. Use: add, replace, remove, read", action), nil
	}

	b, _ := json.Marshal(result)
	return string(b), nil
}

// ── 生命周期 Hook ──

// OnTurnStart 每轮开始时记录日志
func (p *BuiltinProvider) OnTurnStart(turnNumber int, message string, _ map[string]any) {
	preview := message
	if len(preview) > 120 {
		preview = preview[:120] + "..."
	}
	log.Debug("builtin memory: turn start",
		"turn", turnNumber,
		"msg_len", len(message),
		"preview", preview)
}

// OnSessionEnd 会话结束时记录统计
// 内建 Provider 不做自动摘要提取——依赖 LLM 在会话中主动调用 memory 工具
func (p *BuiltinProvider) OnSessionEnd(messages []map[string]any) {
	log.Debug("builtin memory: session end",
		"message_count", len(messages))
}

// OnPreCompress 上下文压缩前，扫描即将丢弃的消息中是否有记忆操作痕迹
// 返回简短提示供 compressor 保留在压缩摘要中
func (p *BuiltinProvider) OnPreCompress(messages []map[string]any) string {
	memOps := 0
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		// 扫描 tool result 中的记忆操作标记
		if role == "tool" || role == "assistant" {
			if strings.Contains(content, "Entry added") ||
				strings.Contains(content, "Entry replaced") ||
				strings.Contains(content, "Entry removed") {
				memOps++
			}
		}
	}

	if memOps == 0 {
		return ""
	}

	return fmt.Sprintf("[Memory: %d memory write operation(s) occurred in the compressed context. "+
		"Current memory state is available via the memory tool with action='read'.]", memOps)
}

// OnMemoryWrite 内建 Provider 是写入源，无需自我镜像，no-op
func (p *BuiltinProvider) OnMemoryWrite(_, _, _ string) {}

// OnDelegation 子 Agent 完成委托时记录日志
func (p *BuiltinProvider) OnDelegation(task, result, childSessionID string) {
	taskPreview := task
	if len(taskPreview) > 80 {
		taskPreview = taskPreview[:80] + "..."
	}
	log.Debug("builtin memory: delegation completed",
		"task", taskPreview,
		"result_len", len(result),
		"child_session", childSessionID)
}

// Shutdown 清理——内建 Provider 无外部资源需要释放
func (p *BuiltinProvider) Shutdown() {
	log.Debug("builtin memory: clean exit")
}

// ── 内部辅助 ──

func errJSON(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	b, _ := json.Marshal(Result{Success: false, Error: msg})
	return string(b)
}
