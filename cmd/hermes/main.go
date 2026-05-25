package main

import (
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/tool/builtin"
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/tool/terminal"
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/tool/web"

	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
	agentctx "code.byted.org/ad_creative/hermes_agent_go/pkg/agent/context"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/credential"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory/mempalace"
	hlog "code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/deepseek"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/openai"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/state"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/builtin"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/delegate"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/mcp"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func main() {
	// 日志级别
	logLevel := slog.LevelInfo
	if os.Getenv("HERMES_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	hlog.Init("", logLevel)

	// 加载配置（文件 + env）
	cfg := types.LoadConfig()
	apiKey := cfg.APIKey
	baseURL := cfg.BaseURL
	modelName := cfg.Model

	// hermes home 目录
	hermesHome := os.Getenv("HERMES_HOME")
	if hermesHome == "" {
		home, _ := os.UserHomeDir()
		hermesHome = filepath.Join(home, ".hermes")
	}
	_ = os.MkdirAll(hermesHome, 0o755)

	// 生成 session ID
	sessionID := fmt.Sprintf("sess_%d", time.Now().UnixMilli())

	// 初始化模型路由
	router := model.NewRouter()
	var mainProvider model.Provider

	// 根据配置中的 model 解析 provider 并创建对应的 Provider
	if modelName != "" {
		providerName, _, _ := strings.Cut(modelName, "/")
		if apiKey != "" {
			switch providerName {
			case "deepseek":
				mainProvider = deepseek.New(apiKey, baseURL)
			default:
				mainProvider = openai.New(apiKey, baseURL)
			}
			router.Register(mainProvider)
		}
	}

	// 注册自定义 Provider（从配置文件 custom_providers 加载）
	for _, cp := range cfg.CustomProviders {
		if cp.Name == "" || cp.APIKey == "" {
			continue
		}
		// 计算该 provider 默认模型的 context length
		maxCtx := 128000
		if cp.Model != "" {
			if mc, ok := cp.Models[cp.Model]; ok && mc.ContextLength > 0 {
				maxCtx = mc.ContextLength
			}
		}
		p := openai.NewWithName(cp.Name, cp.APIKey, cp.BaseURL, maxCtx)
		router.Register(p)
		fmt.Fprintf(os.Stderr, "Custom provider registered: %s (%s)\n", cp.Name, cp.BaseURL)
	}

	// 如果当前 Model 指向的 provider 未注册但已有其他 provider 可用，自动切换
	if _, _, err := router.Resolve(cfg.Model); err != nil {
		if first := router.FirstRegistered(); first != nil {
			oldModel := cfg.Model
			for _, cp := range cfg.CustomProviders {
				if cp.Name == first.Name() && cp.Model != "" {
					cfg.Model = cp.Name + "/" + cp.Model
					break
				}
			}
			if cfg.Model == oldModel {
				cfg.Model = first.Name() + "/" + oldModel
			}
			fmt.Fprintf(os.Stderr, "Model %q unavailable, auto-switched to %q\n", oldModel, cfg.Model)
		}
	}

	// 初始化凭据池（支持多 key 轮转）
	var credPool *credential.Pool
	if extraKeys := os.Getenv("HERMES_API_KEYS"); extraKeys != "" {
		credPool = credential.NewPool(credential.StrategyRoundRobin)
		// 添加主 key
		if apiKey != "" {
			credPool.Add(&credential.Credential{
				ID: "primary", APIKey: apiKey, BaseURL: baseURL, Priority: 0,
			})
		}
		// 添加额外 key（逗号分隔）
		for i, key := range strings.Split(extraKeys, ",") {
			key = strings.TrimSpace(key)
			if key != "" {
				credPool.Add(&credential.Credential{
					ID: fmt.Sprintf("extra_%d", i), APIKey: key, BaseURL: baseURL, Priority: i + 1,
				})
			}
		}
		if mainProvider != nil {
			switch p := mainProvider.(type) {
			case *openai.Provider:
				p.SetCredentialPool(credPool)
			case *deepseek.Provider:
				p.SetCredentialPool(credPool)
			}
		}
		fmt.Fprintf(os.Stderr, "🔑 Credential pool: %d key(s)\n", credPool.AvailableCount())
	}

	// 初始化工具注册表
	reg := registry.Global()

	// Agent 配置补充
	cfg.SessionID = sessionID
	if cfg.WorkDir == "" {
		if wd, err := os.Getwd(); err == nil {
			cfg.WorkDir = wd
		}
	}

	// 创建 Agent
	ag := agent.NewAIAgent(cfg, router, reg)

	// ── 初始化 TODO 规划系统 ──
	todoStore := builtin.NewTodoStore()
	builtin.RegisterTodoTools(todoStore, "main")
	ag.SetTodoStore(todoStore)

	// ── 初始化 SessionDB + TODO 持久化 ──
	dbPath := filepath.Join(hermesHome, "sessions.db")
	sessionDB, err := state.NewSessionDB(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  SessionDB init failed (non-fatal): %v\n", err)
	} else {
		// 初始化 TODO 表
		if err := sessionDB.InitTodoSchema(); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  TODO schema init failed: %v\n", err)
		}

		// 创建会话记录
		_ = sessionDB.CreateSession(&state.Session{
			ID:        sessionID,
			Source:    "cli",
			Model:     modelName,
			StartedAt: time.Now(),
		})

		// 尝试恢复上次 TODO（断点续跑场景，暂时不启用，留接口）
		// if snap, _ := sessionDB.LoadTodos(sessionID); snap != nil { ... }

		// 设置 TODO 持久化回调
		todoStore.SetPersistFn(func(items []builtin.TodoItem) error {
			data, _ := json.Marshal(items)
			return sessionDB.SaveTodos(sessionID, data)
		})
	}

	// ── 初始化 Skill 系统 ──
	skillsDir := filepath.Join(hermesHome, "skills")
	_ = os.MkdirAll(skillsDir, 0o755)
	skillMgr := builtin.NewSkillManager([]string{skillsDir})
	skillMgr.RegisterTool()
	ag.SetSkillManager(skillMgr)
	// ── 初始化上下文压缩器 ──
	if mainProvider != nil {
		compressor := agentctx.NewCompressor(mainProvider, modelName, mainProvider.MaxContextTokens())
		ag.SetCompressor(compressor)
	}

	// ── 注册 delegate_task 工具 ──
	delegateProvider := delegate.NewProvider(ag, 3)
	if err := delegateProvider.Register(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  delegate_task register: %v\n", err)
	}

	// ── 初始化 MCP 服务 ──
	var mcpMgr *mcp.Manager
	mcpConfigs, err := mcp.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  MCP config error: %v\n", err)
	}
	if len(mcpConfigs) > 0 {
		mcpMgr = mcp.NewManager()
		fmt.Fprintf(os.Stderr, "🔌 Connecting to %d MCP server(s)...\n", len(mcpConfigs))
		if err := mcpMgr.DiscoverAndRegister(context.Background(), mcpConfigs); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  MCP: %v\n", err)
		}
		statuses := mcpMgr.GetStatus()
		total := 0
		for _, s := range statuses {
			total += len(s.Tools)
		}
		if total > 0 {
			fmt.Fprintf(os.Stderr, "   %d MCP tool(s) registered from %d server(s)\n", total, len(statuses))
		}
	}

	// ── 初始化记忆系统 ──
	if !cfg.SkipMemory {
		memDir := filepath.Join(hermesHome, "memories")
		store := memory.NewStore(memDir, 0, 0)

		builtinProvider := memory.NewBuiltinProvider(store)

		memMgr := memory.NewManager()
		_ = memMgr.AddProvider(builtinProvider)

		memMgr.InitializeAll(context.Background(), memory.InitOpts{
			SessionID:  cfg.SessionID,
			HermesHome: hermesHome,
			Platform:   cfg.Platform,
		})

		ag.SetMemoryManager(memMgr)
	}

	// ── 初始化 MemPalace 外部记忆 Provider ──
	if os.Getenv("HERMES_MEMPALACE") != "0" {
		palacePath := filepath.Join(hermesHome, "palace")
		palaceProvider := mempalace.New(palacePath)
		if ag.MemoryManager() != nil {
			if err := ag.MemoryManager().AddProvider(palaceProvider); err != nil {
				fmt.Fprintf(os.Stderr, "MemPalace warning: %v\n", err)
			} else {
				palaceProvider.Initialize(context.Background(), memory.InitOpts{
					SessionID:  cfg.SessionID,
					HermesHome: hermesHome,
					Platform:   cfg.Platform,
				})
			}
		}
	}

	// 设置事件回调（CLI 输出）
	streamedThisTurn := false
	ag.SetEventCallback(func(e agent.Event) {
		switch e.Type {
		case agent.EventToolStart:
			fmt.Fprintf(os.Stderr, "  🔧 %s(%s)\n", e.ToolName, truncate(e.ToolArgs, 80))
		case agent.EventToolEnd:
			fmt.Fprintf(os.Stderr, "  ✓ %s\n", e.ToolName)
		case agent.EventStreamDelta:
			fmt.Print(e.Content)
			streamedThisTurn = true
		case agent.EventCompression:
			fmt.Fprintf(os.Stderr, "  📦 %s\n", e.Content)
		case agent.EventBudgetWarn:
			fmt.Fprintf(os.Stderr, "  ⚠️  %s\n", e.Content)
		case agent.EventMemory:
			fmt.Fprintf(os.Stderr, "  🧠 %s\n", e.Content)
		}
	})

	// 处理中断信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\n🛑 Interrupted\n")
		if mcpMgr != nil {
			mcpMgr.ShutdownAll()
		}
		ag.Shutdown()
		if sessionDB != nil {
			sessionDB.Close()
		}
		cancel()
	}()

	// REPL 循环
	fmt.Println("🏛️  Hermes Agent (Go) — type /quit to exit")
	fmt.Printf("   Model: %s | Budget: %d iterations\n", cfg.Model, cfg.MaxIterations)
	fmt.Printf("   Session: %s\n", sessionID)
	fmt.Printf("   Memory: %s/memories\n\n", hermesHome)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(">>> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/quit" || input == "/exit" {
			if mcpMgr != nil {
				mcpMgr.ShutdownAll()
			}
			persistMessages(sessionDB, sessionID, ag)
			ag.Shutdown()
			if sessionDB != nil {
				sessionDB.Close()
			}
			fmt.Println("👋 Bye!")
			break
		}
		if input == "/stats" {
			stats := ag.GetStats()
			fmt.Printf("  Iterations: %d | Tool calls: %d | Tokens: %d in / %d out\n",
				stats.TotalIterations, stats.ToolCalls, stats.InputTokens, stats.OutputTokens)
			continue
		}
		if input == "/budget" {
			fmt.Println("  Budget tracking: managed by graph executor (MaxSteps = cfg.MaxIterations)")
			continue
		}
		if input == "/todo" {
			items := todoStore.Read()
			if len(items) == 0 {
				fmt.Println("  (no TODO list)")
			} else {
				fmt.Println(todoStore.Summary())
			}
			continue
		}
		if input == "/mcp" {
			if mcpMgr == nil {
				fmt.Println("  No MCP servers configured")
			} else {
				for _, s := range mcpMgr.GetStatus() {
					st := "ready"
					if s.Error != "" {
						st = "error: " + s.Error
					}
					fmt.Printf("  [%s] %s (%s) — %d tools\n",
						s.Transport, s.Name, st, len(s.Tools))
				}
			}
			continue
		}

		streamedThisTurn = false
		reply, _, err := ag.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
			continue
		}
		// Stream deltas are already printed via EventStreamDelta callback.
		// Only print reply if it wasn't streamed (i.e., no streaming callback fired).
		if !streamedThisTurn && reply != "" {
			fmt.Println(reply)
		} else {
			fmt.Println() // newline after streamed output
		}
	}
}

// persistMessages 持久化消息历史到 SessionDB
func persistMessages(sessionDB *state.SessionDB, sessionID string, ag *agent.AIAgent) {
	if sessionDB == nil {
		return
	}
	msgs := ag.GetMessages()
	if len(msgs) <= 1 {
		return // 仅系统提示，无需保存
	}
	var saved int
	for _, msg := range msgs[1:] { // 跳过 system prompt
		if msg.Role == "system" {
			continue // 跳过注入的 reminder 消息
		}
		m := msg // copy for pointer
		if err := sessionDB.SaveMessage(sessionID, &m); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  persist msg error: %v\n", err)
			break
		}
		saved++
	}
	if saved > 0 {
		fmt.Fprintf(os.Stderr, "  💾 %d messages persisted\n", saved)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
