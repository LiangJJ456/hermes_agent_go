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
//   1. 工具输出裁剪（无 LLM 调用）
//   2. 头部保护（系统提示 + 首次交换）
//   3. 尾部保护（最新消息）
//   4. 中间部分用 LLM 生成摘要
//   5. 防抖检查
func (c *Compressor) Compress(ctx context.Context, messages []types.Message) ([]types.Message, error) {
	if len(messages) <= headProtectCount+2 {
		return messages, nil // 太短，不压缩
	}

	beforeTokens := estimateTokens(messages)

	// Step 1: 工具输出裁剪
	trimmed := c.trimToolOutputs(messages)

	// Step 2: 分割 head / middle / tail
	head := trimmed[:headProtectCount]
	tail := c.protectTail(trimmed[headProtectCount:], tailProtectTokens)
	middleEnd := len(trimmed) - len(tail)
	middle := trimmed[headProtectCount:middleEnd]

	if len(middle) == 0 {
		return trimmed, nil // 没有可压缩的中间部分
	}

	// Step 3: 用 LLM 生成中间部分摘要
	summary, err := c.summarizeMiddle(ctx, middle)
	if err != nil {
		log.Warn("context compression failed, using trimmed messages", "error", err)
		return trimmed, nil // 降级：仅返回工具输出裁剪后的结果
	}

	// Step 4: 拼装结果
	summaryMsg := types.Message{
		Role:    types.RoleAssistant,
		Content: fmt.Sprintf("%s\n\n%s", summaryPrefix, summary),
	}

	result := make([]types.Message, 0, len(head)+1+len(tail))
	result = append(result, head...)
	result = append(result, summaryMsg)
	result = append(result, tail...)

	// Step 5: 防抖
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
	)

	return result, nil
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

			// 保留首 200 字符作为上下文
			preview := msg.Content
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}

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

// estimateTokens 粗略估算 Token 数（4 chars ≈ 1 token）
func estimateTokens(messages []types.Message) int {
	total := 0
	for _, m := range messages {
		total += estimateMessageTokens(m)
	}
	return total
}

func estimateMessageTokens(m types.Message) int {
	chars := len(m.Content) + len(m.Reasoning)
	for _, tc := range m.ToolCalls {
		chars += len(tc.Function.Arguments) + len(tc.Function.Name)
	}
	return chars/4 + 4 // +4 for role/name overhead
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
