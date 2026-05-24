package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	agentctx "code.byted.org/ad_creative/hermes_agent_go/pkg/agent/context"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/prompt"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/errx"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/builtin"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// EventType Agent 事件类型
type EventType int

const (
	EventToolStart    EventType = iota // 工具开始执行
	EventToolEnd                       // 工具执行完成
	EventStreamDelta                   // 流式增量
	EventCompression                   // 上下文压缩触发
	EventBudgetWarn                    // 预算即将耗尽
	EventMemory                        // 记忆操作
	EventError                         // 错误
)

// Event Agent 运行事件
type Event struct {
	Type      EventType
	ToolName  string
	ToolArgs  string
	Content   string
	Error     error
	Timestamp time.Time
}

// EventCallback 事件回调
type EventCallback func(Event)

// AIAgent 核心 Agent 引擎
type AIAgent struct {
	config   types.AgentConfig
	router   *model.Router
	registry *registry.Registry
	budget   *IterationBudget
	executor *ParallelExecutor

	// 对话状态
	mu       sync.Mutex
	messages []types.Message
	turnNum  int // 当前轮次

	// 上下文管理
	promptBuilder *prompt.Builder
	compressor    *agentctx.Compressor

	// 记忆系统
	memoryMgr *memory.Manager

	// 运行统计
	stats Stats

	// 事件回调
	eventCB EventCallback

	// 规划系统
	todoStore *builtin.TodoStore

	// Skill 系统：skillMgr 为共享的发现/读取后端；activeSkills 是本 agent 私有的激活集合
	skillMgr     *builtin.SkillManager
	activeSkills map[string]bool

	// 子 Agent 控制
	depth    int  // 当前委托深度
	isChild  bool // 是否是子 Agent
}

// Stats 运行统计
type Stats struct {
	TotalIterations int
	ToolCalls       int
	InputTokens     int
	OutputTokens    int
	StartTime       time.Time
	EndTime         time.Time
}

// NewAIAgent 创建 Agent
func NewAIAgent(cfg types.AgentConfig, router *model.Router, reg *registry.Registry) *AIAgent {
	budget := NewIterationBudget(cfg.MaxIterations)

	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}

	pb := prompt.NewBuilder(cfg.Platform, cfg.Model, workDir)

	a := &AIAgent{
		config:        cfg,
		router:        router,
		registry:      reg,
		budget:        budget,
		executor:      NewParallelExecutor(reg, cfg.MaxParallelTools),
		promptBuilder: pb,
		stats:         Stats{StartTime: time.Now()},
	}

	budget.SetOnExhaust(func() {
		a.emitEvent(Event{Type: EventBudgetWarn, Content: "iteration budget exhausted"})
	})

	return a
}

// SetEventCallback 设置事件回调
func (a *AIAgent) SetEventCallback(cb EventCallback) {
	a.eventCB = cb
}

// SetCustomPrompt 设置自定义系统提示
func (a *AIAgent) SetCustomPrompt(p string) {
	a.promptBuilder.SetCustomPrompt(p)
}

// SetMemoryContext 注入记忆上下文（直接设置，不走 Manager）
func (a *AIAgent) SetMemoryContext(memCtx string) {
	a.promptBuilder.SetMemoryContext(memCtx)
}

// SetSkillManager 挂载 skill 发现/读取后端。激活状态是 per-agent 的（见 a.activeSkills），
// 父子 agent 可共享同一个 SkillManager，互不干扰。
func (a *AIAgent) SetSkillManager(sm *builtin.SkillManager) {
	a.skillMgr = sm
}

// setSkillActive 更新本 agent 的激活集合，并就地刷新 system prompt 中的 skill 块。
func (a *AIAgent) setSkillActive(name string, active bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.activeSkills == nil {
		a.activeSkills = make(map[string]bool)
	}
	if active {
		a.activeSkills[name] = true
	} else {
		delete(a.activeSkills, name)
	}
	a.applySkillsBlockLocked()
}

// applySkillsBlockLocked 根据本 agent 的激活集合，就地替换 messages[0] 中的 skill 块。
// 调用方需持有 a.mu。激活集合为空时移除该块。
func (a *AIAgent) applySkillsBlockLocked() {
	if a.skillMgr == nil || len(a.messages) == 0 || a.messages[0].Role != types.RoleSystem {
		return
	}
	section := a.skillMgr.ActiveSection(a.activeSkills)
	base := stripSkillsBlock(a.messages[0].Content)
	if section != "" {
		base = base + "\n\n" + skillsBlockStart + "\n" + section + "\n" + skillsBlockEnd
	}
	a.messages[0].Content = base
}

const (
	skillsBlockStart = "<active-skills>"
	skillsBlockEnd   = "</active-skills>"
)

// stripSkillsBlock 移除 system prompt 中已有的 active-skills 块（含标记），返回剩余内容。
func stripSkillsBlock(content string) string {
	start := strings.Index(content, skillsBlockStart)
	if start == -1 {
		return content
	}
	end := strings.Index(content, skillsBlockEnd)
	if end == -1 || end < start {
		return content
	}
	before := strings.TrimRight(content[:start], "\n")
	after := strings.TrimLeft(content[end+len(skillsBlockEnd):], "\n")
	if after == "" {
		return before
	}
	return before + "\n\n" + after
}

// handleSkillsCall 处理本 agent 的 skills 工具调用（per-agent 激活状态）。
func (a *AIAgent) handleSkillsCall(raw json.RawMessage) string {
	var args struct {
		Action string `json:"action"`
		Name   string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErrJSON("invalid arguments: %v", err)
	}

	switch args.Action {
	case "list":
		return a.skillMgr.ListJSON()
	case "activate":
		if args.Name == "" {
			return toolErrJSON("name is required for activate")
		}
		info, ok := a.skillMgr.Lookup(args.Name)
		if !ok {
			return toolErrJSON("skill '%s' not found. Use action='list' to see available skills.", args.Name)
		}
		a.setSkillActive(args.Name, true)
		log.Info("skills: activated", "name", args.Name, "session", a.config.SessionID)
		return fmt.Sprintf("Skill '%s' activated and added to your system context.\n\n**%s**: %s\n\nUse `skills(action=read, name=%s)` to load its full instructions.",
			args.Name, info.Name, info.Description, args.Name)
	case "deactivate":
		if args.Name == "" {
			return toolErrJSON("name is required for deactivate")
		}
		a.setSkillActive(args.Name, false)
		log.Info("skills: deactivated", "name", args.Name, "session", a.config.SessionID)
		return fmt.Sprintf("Skill '%s' deactivated.", args.Name)
	case "read":
		if args.Name == "" {
			return toolErrJSON("name is required for read")
		}
		a.mu.Lock()
		isActive := a.activeSkills[args.Name]
		a.mu.Unlock()
		if !isActive {
			return toolErrJSON("skill '%s' is not active. Call action='activate' first.", args.Name)
		}
		content, err := a.skillMgr.ReadContent(args.Name)
		if err != nil {
			return toolErrJSON("failed to read skill '%s': %v", args.Name, err)
		}
		return content
	default:
		return toolErrJSON("unknown action '%s'. Use: list, activate, deactivate, read", args.Action)
	}
}

func toolErrJSON(format string, a ...any) string {
	msg := fmt.Sprintf(format, a...)
	b, _ := json.Marshal(map[string]any{"error": msg})
	return string(b)
}

// SetMemoryManager 设置记忆管理器
func (a *AIAgent) SetMemoryManager(mgr *memory.Manager) {
	a.memoryMgr = mgr
}

// MemoryManager 获取记忆管理器
func (a *AIAgent) MemoryManager() *memory.Manager {
	return a.memoryMgr
}

// SetTodoStore 设置 TODO 存储（主 agent 和子 agent 共享同一实例）
func (a *AIAgent) SetTodoStore(store *builtin.TodoStore) {
	a.todoStore = store
}

// TodoStore 获取 TODO 存储
func (a *AIAgent) TodoStore() *builtin.TodoStore {
	return a.todoStore
}

// Run 执行一次完整对话，返回最终 assistant 回复
func (a *AIAgent) Run(ctx context.Context, userInput string) (string, error) {
	a.mu.Lock()

	// 首次运行：初始化
	if len(a.messages) == 0 {
		// 注入记忆系统的 system prompt 块
		if a.memoryMgr != nil {
			memPrompt := a.memoryMgr.BuildSystemPrompt()
			if memPrompt != "" {
				a.promptBuilder.SetMemoryContext(memPrompt)
			}
		}

		sysMsg := a.promptBuilder.Build()
		a.messages = []types.Message{sysMsg}
	}

	// 追加用户消息
	a.messages = append(a.messages, types.Message{
		Role:      types.RoleUser,
		Content:   userInput,
		Timestamp: time.Now(),
	})
	a.turnNum++
	a.mu.Unlock()

	// pre-turn: 记忆预取
	if a.memoryMgr != nil {
		a.memoryMgr.OnTurnStart(a.turnNum, userInput, nil)
		memCtx := a.memoryMgr.PrefetchAll(ctx, userInput, a.config.SessionID)
		if memCtx != "" {
			// 构建围栏上下文块，注入到最新消息前（API 调用时注入，不持久化）
			a.emitEvent(Event{Type: EventMemory, Content: "memory context recalled"})
			_ = memCtx // 上下文注入在 conversationLoop 中处理
		}
	}

	// 核心对话循环
	reply, err := a.conversationLoop(ctx)

	// post-turn: 同步记忆
	if err == nil && a.memoryMgr != nil {
		a.memoryMgr.SyncAll(userInput, reply, a.config.SessionID)
		a.memoryMgr.QueuePrefetchAll(userInput, a.config.SessionID)
	}

	return reply, err
}

// conversationLoop 核心对话循环
func (a *AIAgent) conversationLoop(ctx context.Context) (string, error) {
	provider, modelName, err := a.router.Resolve(a.config.Model)
	if err != nil {
		return "", fmt.Errorf("resolve model: %w", err)
	}

	// 获取工具 Schema
	toolSchemas := a.registry.GetSchemas(a.config.EnabledToolsets, a.config.DisabledTools)

	// 合并记忆工具 Schema（修复 BUG: 之前未将记忆工具暴露给模型）
	if a.memoryMgr != nil {
		for _, ms := range a.memoryMgr.GetAllToolSchemas() {
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

	for {
		// 检查上下文取消
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		// 检查预算
		if a.budget.Remaining() <= 0 {
			return "", errx.ErrBudgetExhausted
		}

		// 上下文压缩检查
		a.maybeCompress(ctx)

		// 构建请求
		a.mu.Lock()
		msgs := make([]types.Message, len(a.messages))
		copy(msgs, a.messages)
		a.mu.Unlock()

		// 注入记忆预取上下文（在 API 调用时注入，不持久化到 a.messages）
		if a.memoryMgr != nil {
			memCtx := a.memoryMgr.PrefetchAll(ctx, "", a.config.SessionID)
			if memCtx != "" {
				contextBlock := memory.BuildContextBlock(memCtx)
				if contextBlock != "" {
					// 在用户消息之前插入一条记忆上下文消息
					injected := make([]types.Message, 0, len(msgs)+1)
					// 找到最后一条用户消息的位置
					lastUserIdx := -1
					for i := len(msgs) - 1; i >= 0; i-- {
						if msgs[i].Role == types.RoleUser {
							lastUserIdx = i
							break
						}
					}
					if lastUserIdx > 0 {
						injected = append(injected, msgs[:lastUserIdx]...)
						injected = append(injected, types.Message{
							Role:    types.RoleSystem,
							Content: contextBlock,
						})
						injected = append(injected, msgs[lastUserIdx:]...)
						msgs = injected
					}
				}
			}
		}

		// 注入 TODO reminder（临时塞到队尾，不持久化到 a.messages）
		// 每轮重新生成，反映最新计划状态；持久化历史保持纯 append，前缀稳定，cache 友好
		if a.todoStore != nil {
			if todoSummary := a.todoStore.Summary(); todoSummary != "" {
				msgs = append(msgs, types.Message{
					Role:    types.RoleSystem,
					Content: "<todo-reminder>\n" + todoSummary + "\nContinue with the next pending task. Mark it in_progress before starting.\n</todo-reminder>",
				})
			}
		}

		req := &model.ChatRequest{
			Model:       modelName,
			Messages:    msgs,
			Tools:       toolSchemas,
			Temperature: a.config.Temperature,
			MaxTokens:   a.config.MaxTokens,
		}

		// 调用 LLM
		var resp *types.ChatResponse
		if a.eventCB != nil {
			// 流式
			resp, err = provider.ChatStream(ctx, req, func(delta types.StreamDelta) {
				if delta.Content != "" {
					a.emitEvent(Event{
						Type:    EventStreamDelta,
						Content: delta.Content,
					})
				}
				if delta.ToolCallStart {
					a.emitEvent(Event{
						Type:     EventToolStart,
						ToolName: delta.ToolCallName,
						Content:  "streaming tool_call detected",
					})
				}
			})
		} else {
			// 同步
			resp, err = provider.Chat(ctx, req)
		}
		if err != nil {
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		// 更新统计
		a.stats.InputTokens += resp.Usage.PromptTokens
		a.stats.OutputTokens += resp.Usage.CompletionTokens

		// 追加 assistant 消息
		assistantMsg := resp.Message
		assistantMsg.Timestamp = time.Now()
		a.mu.Lock()
		a.messages = append(a.messages, assistantMsg)
		a.mu.Unlock()

		// 判断是否有工具调用
		if len(assistantMsg.ToolCalls) == 0 {
			// 无工具调用 → 对话结束
			a.stats.EndTime = time.Now()
			return assistantMsg.Content, nil
		}

		// 消耗预算
		if !a.budget.Consume() {
			return "", errx.ErrBudgetExhausted
		}
		a.stats.TotalIterations++

		// Budget 预留提醒：剩余 < 5 时注入警告
		if remaining := a.budget.Remaining(); remaining > 0 && remaining < 5 {
			a.mu.Lock()
			a.messages = append(a.messages, types.Message{
				Role:    types.RoleSystem,
				Content: fmt.Sprintf("<budget-warning>IMPORTANT: Only %d iterations remaining. Wrap up your current task immediately. Complete or summarize your work, do NOT start new subtasks.</budget-warning>", remaining),
			})
			a.mu.Unlock()
			a.emitEvent(Event{Type: EventBudgetWarn, Content: fmt.Sprintf("budget low: %d remaining", remaining)})
		}

		// 执行工具调用（包含记忆工具路由）
		results := a.executeToolCalls(ctx, assistantMsg.ToolCalls)

		// 追加工具结果消息
		a.mu.Lock()
		for _, r := range results {
			a.messages = append(a.messages, types.Message{
				Role:       types.RoleTool,
				Content:    r.Result.Content,
				ToolCallID: r.Result.ToolCallID,
				Name:       r.Call.Function.Name,
				Timestamp:  time.Now(),
			})
			a.stats.ToolCalls++
		}
		a.mu.Unlock()

		// 继续循环，让 LLM 看到工具结果
	}
}

// executeToolCalls 执行一批工具调用（可能并行）
func (a *AIAgent) executeToolCalls(ctx context.Context, calls []types.ToolCall) []toolCallWithResult {
	// 发出工具开始事件
	for _, c := range calls {
		a.emitEvent(Event{
			Type:     EventToolStart,
			ToolName: c.Function.Name,
			ToolArgs: c.Function.Arguments,
		})
	}

	// 检查是否有记忆工具调用需要路由到 MemoryManager
	results := make([]toolCallWithResult, len(calls))
	var nonMemoryCalls []int // 非记忆工具的索引

	for i, c := range calls {
		if a.skillMgr != nil && c.Function.Name == "skills" {
			// skills 工具：per-agent 激活状态，由本 agent 处理（不走全局 registry handler）
			results[i] = toolCallWithResult{
				Call: c,
				Result: types.ToolResult{
					ToolCallID: c.ID,
					Content:    a.handleSkillsCall(json.RawMessage(c.Function.Arguments)),
				},
			}
		} else if a.memoryMgr != nil && a.memoryMgr.HasTool(c.Function.Name) {
			// 记忆工具：直接路由到 MemoryManager
			var args map[string]any
			if err := json.Unmarshal([]byte(c.Function.Arguments), &args); err != nil {
				results[i] = toolCallWithResult{
					Call: c,
					Result: types.ToolResult{
						ToolCallID: c.ID,
						Content:    fmt.Sprintf("Error: invalid arguments: %v", err),
						IsError:    true,
					},
				}
				continue
			}

			output, err := a.memoryMgr.HandleToolCall(ctx, c.Function.Name, args)
			if err != nil {
				results[i] = toolCallWithResult{
					Call: c,
					Result: types.ToolResult{
						ToolCallID: c.ID,
						Content:    fmt.Sprintf("Error: %v", err),
						IsError:    true,
					},
				}
			} else {
				results[i] = toolCallWithResult{
					Call: c,
					Result: types.ToolResult{
						ToolCallID: c.ID,
						Content:    output,
					},
				}
				// 通知外部 Provider 镜像写入
				action, _ := args["action"].(string)
				target, _ := args["target"].(string)
				content, _ := args["content"].(string)
				if action == "add" || action == "replace" || action == "remove" {
					a.memoryMgr.OnMemoryWrite(action, target, content)
				}
			}
		} else {
			nonMemoryCalls = append(nonMemoryCalls, i)
		}
	}

	// 非记忆工具：走 ParallelExecutor
	if len(nonMemoryCalls) > 0 {
		var batchCalls []types.ToolCall
		for _, idx := range nonMemoryCalls {
			batchCalls = append(batchCalls, calls[idx])
		}
		batchResults := a.executor.ExecuteBatch(ctx, batchCalls)
		for j, idx := range nonMemoryCalls {
			results[idx] = batchResults[j]
		}
	}

	// 发出工具完成事件
	for _, r := range results {
		a.emitEvent(Event{
			Type:     EventToolEnd,
			ToolName: r.Call.Function.Name,
			Content:  truncateResult(r.Result.Content, 200),
		})
	}

	return results
}

// maybeCompress 上下文压缩检查
func (a *AIAgent) maybeCompress(ctx context.Context) {
	if a.compressor == nil {
		return
	}
	a.mu.Lock()
	msgs := a.messages
	a.mu.Unlock()

	if !a.compressor.NeedsCompression(msgs) {
		return
	}

	// 压缩前通知记忆系统提取洞察
	if a.memoryMgr != nil {
		msgMaps := messagesToMaps(msgs)
		insight := a.memoryMgr.OnPreCompress(msgMaps)
		_ = insight // TODO: 传递给 compressor 的摘要 prompt
	}

	a.emitEvent(Event{Type: EventCompression, Content: "compressing context"})

	compressed, err := a.compressor.Compress(ctx, msgs)
	if err != nil {
		log.Warn("compression failed", "error", err)
		return
	}

	a.mu.Lock()
	a.messages = compressed
	a.mu.Unlock()
}

// Shutdown 清理退出
func (a *AIAgent) Shutdown() {
	// 通知记忆系统会话结束
	if a.memoryMgr != nil {
		msgMaps := messagesToMaps(a.GetMessages())
		a.memoryMgr.OnSessionEnd(msgMaps)
		a.memoryMgr.ShutdownAll()
	}
}

// SetCompressor 设置上下文压缩器
func (a *AIAgent) SetCompressor(c *agentctx.Compressor) {
	a.compressor = c
}

// GetMessages 获取当前对话历史（只读副本）
func (a *AIAgent) GetMessages() []types.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]types.Message, len(a.messages))
	copy(out, a.messages)
	return out
}

// GetStats 获取运行统计
func (a *AIAgent) GetStats() Stats {
	return a.stats
}

// Budget 获取预算
func (a *AIAgent) Budget() *IterationBudget {
	return a.budget
}

func (a *AIAgent) emitEvent(e Event) {
	e.Timestamp = time.Now()
	if a.eventCB != nil {
		a.eventCB(e)
	}
}

func truncateResult(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ── 子 Agent 支持 ──

// NewChildAgent 创建子 Agent（用于 delegate_task）
func (a *AIAgent) NewChildAgent(task string) (*AIAgent, error) {
	if a.depth >= a.config.MaxDelegateDepth {
		return nil, errx.ErrDelegateDepthExceeded
	}

	childCfg := a.config
	childCfg.MaxIterations = a.config.DelegateMaxIterations
	childCfg.SessionID = fmt.Sprintf("%s-child-%d", a.config.SessionID, time.Now().UnixMilli())
	childCfg.SkipMemory = true // 子 Agent 不加载记忆

	// 子 Agent 禁用的工具
	childCfg.DisabledTools = append(append([]string{}, childCfg.DisabledTools...),
		"delegate_task", // 防递归
		"clarify",       // 无交互
		"memory",        // 防共享写入
	)

	child := NewAIAgent(childCfg, a.router, a.registry)
	child.depth = a.depth + 1
	child.isChild = true
	child.todoStore = a.todoStore // 子 agent 共享 TODO 状态
	child.skillMgr = a.skillMgr   // 共享发现后端；激活集合各自独立（activeSkills 默认空）

	// 子 Agent 继承父 Agent 的事件回调（带前缀）
	if a.eventCB != nil {
		child.SetEventCallback(func(e Event) {
			e.Content = fmt.Sprintf("[child-agent] %s", e.Content)
			a.eventCB(e)
		})
	}

	return child, nil
}

// IsChild 是否是子 Agent
func (a *AIAgent) IsChild() bool {
	return a.isChild
}

// Depth 当前委托深度
func (a *AIAgent) Depth() int {
	return a.depth
}

// ── 序列化支持 ──

// MessageHistoryJSON 导出对话历史为 JSON
func (a *AIAgent) MessageHistoryJSON() ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return json.Marshal(a.messages)
}

// ── 辅助 ──

func messagesToMaps(msgs []types.Message) []map[string]any {
	result := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		result[i] = map[string]any{
			"role":    string(m.Role),
			"content": m.Content,
		}
		if m.Name != "" {
			result[i]["name"] = m.Name
		}
	}
	return result
}
