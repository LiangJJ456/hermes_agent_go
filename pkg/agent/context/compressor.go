package context

import (
	"context"
	"fmt"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	// 触发压缩的上下文窗口占比阈值
	compressionThreshold = 0.5
	// 头部保护：保留系统提示 + 前 N 条消息
	headProtectCount = 3
	// 尾部保护 Token 预算
	tailProtectTokens = 20000
	// 防抖：连续压缩节省 < 10% 则跳过
	debounceRatio = 0.10
	// 摘要前缀标记
	summaryPrefix = "[CONTEXT COMPACTION — REFERENCE ONLY]"
	// TODO reminder 标记
	todoReminderTag = "<todo-reminder>"
)

// Compressor 上下文压缩器
type Compressor struct {
	provider     model.Provider
	modelName    string
	maxCtxTokens int
	lastSavings  float64 // 上一次压缩的节省比例
}

// NewCompressor 创建压缩器
// summaryProvider 为用于生成摘要的 LLM Provider（建议用便宜/快速模型）
func NewCompressor(provider model.Provider, modelName string, maxCtxTokens int) *Compressor {
	return &Compressor{
		provider:     provider,
		modelName:    modelName,
		maxCtxTokens: maxCtxTokens,
	}
}

// NeedsCompression 判断是否需要压缩
func (c *Compressor) NeedsCompression(messages []types.Message) bool {
	totalTokens := estimateTokens(messages)
	return float64(totalTokens) > float64(c.maxCtxTokens)*compressionThreshold
}

// Compress 执行压缩，返回压缩后的消息列表
// 算法：
//  1. 工具输出裁剪（无 LLM 调用）
//  2. 头部保护（系统提示 + 首次交换）
//  3. 提取并保护最后一条 TODO reminder
//  4. 尾部保护（最新消息）
//  5. 中间部分用 LLM 生成摘要
//  6. 防抖检查
func (c *Compressor) Compress(ctx context.Context, messages []types.Message) ([]types.Message, error) {
	if len(messages) <= headProtectCount+2 {
		return messages, nil // 太短，不压缩
	}

	beforeTokens := estimateTokens(messages)

	// Step 1: 工具输出裁剪
	trimmed := c.trimToolOutputs(messages)

	// Step 2: 提取最后一条 TODO reminder（从中间部分保护它）
	lastTodoReminder := c.extractLastTodoReminder(trimmed)

	// Step 3: 分割 head / middle / tail
	head := trimmed[:headProtectCount]
	tail := c.protectTail(trimmed[headProtectCount:], tailProtectTokens)
	middleEnd := len(trimmed) - len(tail)
	middle := trimmed[headProtectCount:middleEnd]

	if len(middle) == 0 {
		return trimmed, nil // 没有可压缩的中间部分
	}

	// 从 middle 中移除 TODO reminder 消息（它们会被单独保护）
	middle = c.removeTodoReminders(middle)

	// Step 4: 用 LLM 生成中间部分摘要
	summary, err := c.summarizeMiddle(ctx, middle)
	if err != nil {
		log.Warn("context compression failed, using trimmed messages", "error", err)
		return trimmed, nil // 降级：仅返回工具输出裁剪后的结果
	}

	// Step 5: 拼装结果
	summaryMsg := types.Message{
		Role:    types.RoleAssistant,
		Content: fmt.Sprintf("%s\n\n%s", summaryPrefix, summary),
	}

	result := make([]types.Message, 0, len(head)+3+len(tail))
	result = append(result, head...)
	result = append(result, summaryMsg)
	// 在摘要之后、tail 之前插入保护的 TODO reminder
	if lastTodoReminder != nil {
		result = append(result, *lastTodoReminder)
	}
	result = append(result, tail...)

	// Step 6: 防抖
	afterTokens := estimateTokens(result)
	savings := 1.0 - float64(afterTokens)/float64(beforeTokens)
	if savings < debounceRatio && c.lastSavings < debounceRatio {
		log.Info("compression debounce: savings too low, skipping",
			"savings_pct", fmt.Sprintf("%.1f%%", savings*100))
		return trimmed, nil
	}
	c.lastSavings = savings

	log.Info("context compressed",
		"before_tokens", beforeTokens,
		"after_tokens", afterTokens,
		"savings_pct", fmt.Sprintf("%.1f%%", savings*100),
		"middle_msgs", len(middle),
		"todo_protected", lastTodoReminder != nil,
	)

	return result, nil
}

// extractLastTodoReminder 从消息列表中找到最后一条 TODO reminder
func (c *Compressor) extractLastTodoReminder(messages []types.Message) *types.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if isTodoReminder(messages[i]) {
			msg := messages[i]
			return &msg
		}
	}
	return nil
}

// removeTodoReminders 从消息列表中移除所有 TODO reminder（压缩时不需要摘要它们）
func (c *Compressor) removeTodoReminders(messages []types.Message) []types.Message {
	result := make([]types.Message, 0, len(messages))
	for _, msg := range messages {
		if !isTodoReminder(msg) {
			result = append(result, msg)
		}
	}
	return result
}

// isTodoReminder 判断消息是否是 TODO reminder
func isTodoReminder(msg types.Message) bool {
	return msg.Role == types.RoleSystem && strings.Contains(msg.Content, todoReminderTag)
}

// trimToolOutputs 将旧的工具结果替换为信息丰富的一行摘要
func (c *Compressor) trimToolOutputs(messages []types.Message) []types.Message {
	result := make([]types.Message, len(messages))
	for i, msg := range messages {
		if msg.Role == types.RoleTool && len(msg.Content) > 500 {
			// 生成摘要行
			lines := strings.Count(msg.Content, "\n")
			toolName := msg.Name
			if toolName == "" {
				toolName = "tool"
			}
			summary := fmt.Sprintf("[%s] output: %d chars, %d lines",
				toolName, len(msg.Content), lines)

			// 保留首 200 字符作为上下文（rune 安全，避免切断多字节字符）
			preview := truncate(msg.Content, 200)

			result[i] = types.Message{
				Role:       types.RoleTool,
				Content:    summary + "\n" + preview,
				Name:       msg.Name,
				ToolCallID: msg.ToolCallID,
			}
		} else {
			result[i] = msg
		}
	}
	return result
}

// protectTail 从尾部保护指定 Token 预算的消息
// P3-18: 确保 tool_call_id 链完整性 — 如果 tail 包含 tool result，
// 则对应的 assistant tool_calls 消息也必须在 tail 中
func (c *Compressor) protectTail(messages []types.Message, budget int) []types.Message {
	if len(messages) == 0 {
		return nil
	}

	tokens := 0
	startIdx := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := estimateMessageTokens(messages[i])
		if tokens+msgTokens > budget {
			break
		}
		tokens += msgTokens
		startIdx = i
	}

	// 确保 tool_call chain 完整性：
	// 如果 tail 的第一条是 tool result，往前找对应的 assistant tool_calls 消息
	for startIdx > 0 {
		firstMsg := messages[startIdx]
		if firstMsg.Role != types.RoleTool || firstMsg.ToolCallID == "" {
			break
		}
		// 往前找包含此 tool_call_id 的 assistant 消息
		found := false
		for j := startIdx - 1; j >= 0; j-- {
			if messages[j].Role == types.RoleAssistant && len(messages[j].ToolCalls) > 0 {
				for _, tc := range messages[j].ToolCalls {
					if tc.ID == firstMsg.ToolCallID {
						startIdx = j
						found = true
						break
					}
				}
				if found {
					break
				}
			}
		}
		if !found {
			break
		}
	}

	return messages[startIdx:]
}

// summarizeMiddle 用 LLM 生成中间消息的结构化摘要
func (c *Compressor) summarizeMiddle(ctx context.Context, middle []types.Message) (string, error) {
	var sb strings.Builder
	for _, msg := range middle {
		sb.WriteString(fmt.Sprintf("[%s]: %s\n", msg.Role, truncate(msg.Content, 300)))
	}

	req := &model.ChatRequest{
		Model: c.modelName,
		Messages: []types.Message{
			{
				Role: types.RoleSystem,
				Content: `You are a conversation summarizer. Summarize the following conversation segment into a concise, structured summary. 
Preserve: key decisions, file paths, tool results, error messages, code changes.
Omit: verbose tool outputs, redundant exchanges, thinking/reasoning.
Format: bullet points grouped by topic.`,
			},
			{
				Role:    types.RoleUser,
				Content: "Summarize this conversation segment:\n\n" + sb.String(),
			},
		},
		Temperature: 0.3,
		MaxTokens:   2000,
	}

	resp, err := c.provider.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summary LLM call failed: %w", err)
	}
	return resp.Message.Content, nil
}

// ── 辅助函数 ──

// estimateTokens 粗略估算消息列表的 Token 数。
func estimateTokens(messages []types.Message) int {
	total := 0
	for _, m := range messages {
		total += estimateMessageTokens(m)
	}
	return total
}

func estimateMessageTokens(m types.Message) int {
	tokens := estimateTextTokens(m.Content) + estimateTextTokens(m.Reasoning)
	for _, tc := range m.ToolCalls {
		tokens += estimateTextTokens(tc.Function.Arguments) + estimateTextTokens(tc.Function.Name)
	}
	return tokens + 4 // +4 for role/name overhead
}

// estimateTextTokens 按字符脚本估算 Token 数。
// 注意：必须按 rune 计数而非字节——len() 对 UTF-8 多字节字符（如中文，3 字节/字）
// 会把汉字算成 ~0.75 token，而实际约 1-2 token，导致严重低估、压缩触发过晚。
// 启发式：ASCII 约 4 字符/token；非 ASCII（主要是 CJK）约 1 token/字（略偏保守）。
func estimateTextTokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r <= 0x7F {
			ascii++
		} else {
			other++
		}
	}
	return ascii/4 + other
}

// truncate 截断到最多 maxRunes 个字符（按 rune 计），超出则追加 "..."。
// 按 rune 边界切，避免把多字节字符（如中文）从中间切断产生非法 UTF-8。
func truncate(s string, maxRunes int) string {
	if maxRunes < 0 {
		maxRunes = 0
	}
	count := 0
	for i := range s { // range 在每个 rune 起始字节处给出索引 i
		if count == maxRunes {
			return s[:i] + "..."
		}
		count++
	}
	return s // rune 总数 <= maxRunes，无需截断
}
