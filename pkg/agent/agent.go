package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/adapters"
	agentctx "code.byted.org/ad_creative/hermes_agent_go/pkg/agent/context"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/prompt"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/errx"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
	orchexec "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/executor"
	orchrunner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
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

	// Orchestrator
	graph       *orchestrator.Graph
	executor    *orchexec.Executor
	llmInvoker  *adapters.RouterAdapter
	toolInvoker *adapters.RegistryAdapter

	// 对话状态
	mu       sync.Mutex
	messages []types.Message
	convMem  *orchcontext.ConversationMemory
	turnNum  int

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
	depth   int
	isChild bool
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
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}

	pb := prompt.NewBuilder(cfg.Platform, cfg.Model, workDir)

	// Build default graph
	var graph *orchestrator.Graph
	var err error
	graph, err = BuildDefaultGraph(cfg)
	if err != nil {
		// Fallback: minimal graph
		graph = &orchestrator.Graph{
			StartAt: "end",
			Nodes: map[string]*orchestrator.NodeSpec{
				"end": {Type: "end"},
			},
		}
	}

	a := &AIAgent{
		config:        cfg,
		router:        router,
		registry:      reg,
		graph:         graph,
		executor:      orchexec.NewExecutor(nil), // tracer set later via SetEventCallback
		promptBuilder: pb,
		convMem:       &orchcontext.ConversationMemory{SessionID: cfg.SessionID},
		stats:         Stats{StartTime: time.Now()},
	}

	// Build adapters
	a.llmInvoker = &adapters.RouterAdapter{
		Router:    router,
		Registry:  reg,
		Config:    cfg,
		MemoryMgr: nil, // set after SetMemoryManager
	}
	a.toolInvoker = &adapters.RegistryAdapter{
		Registry:  reg,
		MemoryMgr: nil, // set after SetMemoryManager
	}

	// Wire invokers to runners
	a.wireRunners()

	return a
}

// wireRunners connects adapters to orchestrator runners.
func (a *AIAgent) wireRunners() {
	if entry, ok := orchestrator.LookupNodeType("llm"); ok {
		if r, ok := entry.Runner.(*orchrunner.LLMRunner); ok {
			r.SetInvoker(a.llmInvoker)
		}
	}
	if entry, ok := orchestrator.LookupNodeType("tool"); ok {
		if r, ok := entry.Runner.(*orchrunner.ToolRunner); ok {
			r.SetInvoker(a.toolInvoker)
		}
	}
	if entry, ok := orchestrator.LookupNodeType("parallel"); ok {
		if r, ok := entry.Runner.(*orchrunner.ParallelRunner); ok {
			r.SetExecutor(a.executor)
		}
	}
}

// SetEventCallback 设置事件回调
func (a *AIAgent) SetEventCallback(cb EventCallback) {
	a.eventCB = cb
	a.executor.Tracer = &eventTracer{cb: cb}
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
	a.llmInvoker.MemoryMgr = mgr
	a.toolInvoker.MemoryMgr = mgr
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

// Run executes one turn of the agent loop using the graph executor.
// Returns (reply, pending, error). pending=true means waiting for human input.
func (a *AIAgent) Run(ctx context.Context, userInput string) (string, bool, error) {
	a.mu.Lock()

	// First run: initialize system prompt
	if len(a.messages) == 0 {
		if a.memoryMgr != nil {
			memPrompt := a.memoryMgr.BuildSystemPrompt()
			if memPrompt != "" {
				a.promptBuilder.SetMemoryContext(memPrompt)
			}
		}
		sysMsg := a.promptBuilder.Build()
		a.messages = []types.Message{sysMsg}
	}

	// Append user message
	a.messages = append(a.messages, types.Message{
		Role:      types.RoleUser,
		Content:   userInput,
		Timestamp: time.Now(),
	})
	a.turnNum++
	a.mu.Unlock()

	// PRE: memory prefetch — called ONCE with real user input (BUG FIX)
	if a.memoryMgr != nil {
		a.memoryMgr.OnTurnStart(a.turnNum, userInput, nil)
		memCtx := a.memoryMgr.PrefetchAll(ctx, userInput, a.config.SessionID)
		if memCtx != "" {
			a.emitEvent(Event{Type: EventMemory, Content: "memory context recalled"})
			// Inject into conversation memory as a system message before the user message
			contextBlock := memory.BuildContextBlock(memCtx)
			if contextBlock != "" {
				a.convMem.AddMessage(orchcontext.Message{
					Role:    "system",
					Content: contextBlock,
				})
			}
		}
	}

	// Sync messages to ConversationMemory for the executor
	a.mu.Lock()
	a.convMem.Messages = messagesToOrchMessages(a.messages)
	a.mu.Unlock()

	// Set system prompt on the LLM node
	if llmNode, ok := a.graph.Nodes["llm"]; ok {
		if llmCfg, ok := llmNode.ParsedConfig.(*orchrunner.LLMConfig); ok {
			sysPrompt := a.promptBuilder.Build().Content
			llmCfg.SystemPrompt = sysPrompt
		}
	}

	// Execute the graph
	output, snap, err := a.executor.Execute(ctx, a.graph, userInput)
	if err != nil {
		return "", false, err
	}

	// Handle interrupt (human-in-the-loop)
	if snap != nil {
		reply := formatOutput(output)
		return reply, true, nil
	}

	// POST: extract reply and sync memory
	reply := formatOutput(output)

	// Add assistant response to message history
	a.mu.Lock()
	if outputMap, ok := output.(map[string]interface{}); ok {
		if content, ok := outputMap["content"].(string); ok && content != "" {
			a.messages = append(a.messages, types.Message{
				Role:      types.RoleAssistant,
				Content:   content,
				Timestamp: time.Now(),
			})
		}
	}
	a.mu.Unlock()

	if a.memoryMgr != nil {
		a.memoryMgr.SyncAll(userInput, reply, a.config.SessionID)
		a.memoryMgr.QueuePrefetchAll(userInput, a.config.SessionID)
	}

	return reply, false, nil
}

// Resume continues execution after human input.
func (a *AIAgent) Resume(ctx context.Context, humanResponse interface{}) (string, bool, error) {
	output, snap, err := a.executor.Resume(ctx, a.graph, nil, humanResponse)
	if err != nil {
		return "", false, err
	}
	if snap != nil {
		return formatOutput(output), true, nil
	}
	return formatOutput(output), false, nil
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

func formatOutput(output interface{}) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	if m, ok := output.(map[string]interface{}); ok {
		if content, ok := m["content"].(string); ok {
			return content
		}
		if msg, ok := m["Message"].(string); ok && msg != "" {
			return msg
		}
	}
	b, _ := json.Marshal(output)
	return string(b)
}

func messagesToOrchMessages(msgs []types.Message) []orchcontext.Message {
	result := make([]orchcontext.Message, len(msgs))
	for i, m := range msgs {
		result[i] = orchcontext.Message{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
	}
	return result
}
