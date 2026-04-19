package main

import (
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/tool/builtin"
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory/mempalace"
	hlog "code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/mcp"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model/openai"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func main() {
	hlog.Init("", slog.LevelInfo)

	// 读取环境变量
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	modelName := os.Getenv("HERMES_MODEL")
	if modelName == "" {
		modelName = "openai/gpt-4o"
	}

	// hermes home 目录
	hermesHome := os.Getenv("HERMES_HOME")
	if hermesHome == "" {
		home, _ := os.UserHomeDir()
		hermesHome = filepath.Join(home, ".hermes")
	}

	// 初始化模型路由
	router := model.NewRouter()
	if apiKey != "" {
		router.Register(openai.New(apiKey, baseURL))
	}

	// 初始化工具注册表
	reg := registry.Global()

	// Agent 配置
	cfg := types.DefaultAgentConfig()
	cfg.Model = modelName

	if wd, err := os.Getwd(); err == nil {
		cfg.WorkDir = wd
	}

	// 创建 Agent
	ag := agent.NewAIAgent(cfg, router, reg)

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
		// 打印已注册的 MCP 工具数
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
		store := memory.NewStore(memDir, 0, 0) // 使用默认限额

		builtinProvider := memory.NewBuiltinProvider(store)

		memMgr := memory.NewManager()
		_ = memMgr.AddProvider(builtinProvider)

		// 初始化
		memMgr.InitializeAll(context.Background(), memory.InitOpts{
			SessionID:  cfg.SessionID,
			HermesHome: hermesHome,
			Platform:   cfg.Platform,
		})

		ag.SetMemoryManager(memMgr)
	}

	// -- 初始化 MemPalace 外部记忆 Provider --
	if os.Getenv("HERMES_MEMPALACE") != "0" {
		palacePath := filepath.Join(hermesHome, "palace")
		palaceProvider := mempalace.New(palacePath)
		if ag.MemoryManager() != nil {
			if err := ag.MemoryManager().AddProvider(palaceProvider); err != nil {
				fmt.Fprintf(os.Stderr, "MemPalace warning: %v\n", err)
			}
		}
	}

	// 设置事件回调（CLI 输出）
	ag.SetEventCallback(func(e agent.Event) {
		switch e.Type {
		case agent.EventToolStart:
			fmt.Fprintf(os.Stderr, "  🔧 %s(%s)\n", e.ToolName, truncate(e.ToolArgs, 80))
		case agent.EventToolEnd:
			fmt.Fprintf(os.Stderr, "  ✓ %s\n", e.ToolName)
		case agent.EventStreamDelta:
			fmt.Print(e.Content)
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
		cancel()
	}()

	// REPL 循环
	fmt.Println("🏛️  Hermes Agent (Go) — type /quit to exit")
	fmt.Printf("   Model: %s | Budget: %d iterations\n", cfg.Model, cfg.MaxIterations)
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
			ag.Shutdown()
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
			fmt.Printf("  Budget: %d/%d remaining\n", ag.Budget().Remaining(), ag.Budget().Max())
			continue
		}
		if input == "/mcp" {
			if mcpMgr == nil {
				fmt.Println("  No MCP servers configured")
			} else {
				for _, s := range mcpMgr.GetStatus() {
					state := "ready"
					if s.Error != "" {
						state = "error: " + s.Error
					}
					fmt.Printf("  [%s] %s (%s) — %d tools\n",
						s.Transport, s.Name, state, len(s.Tools))
				}
			}
			continue
		}

		reply, err := ag.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
			continue
		}
		fmt.Printf("\n%s\n\n", reply)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
