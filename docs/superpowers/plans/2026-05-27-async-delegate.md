# Async Delegate Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `delegate_task_async` tool so the parent agent can dispatch child agents in the background without blocking, with results returned via XML `<task-notification>` messages injected into the parent's next conversation turn.

**Architecture:** `AIAgent` gains a `pendingNotifs []string` slice (mutex-protected) and a buffered signal channel `notifCh`. `delegate_task_async` spawns a goroutine, returns immediately with a task ID, and the goroutine pushes an XML notification on completion. The REPL switches from a blocking scanner to a `select` loop that monitors both stdin and `notifCh`, auto-triggering `agent.Run(ctx, "")` when a notification arrives.

**Tech Stack:** Go stdlib (sync, context, fmt, time); no new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-27-async-delegate-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `pkg/agent/agent.go` | Add notification fields, `AddNotification`, `NotifCh`, drain logic in `Run` |
| Create | `pkg/agent/async_notif_test.go` | Unit tests for notification fields and `Run` drain/guard |
| Modify | `pkg/tool/delegate/delegate.go` | Extract `buildFullTask`, add `buildTaskNotification`, add `handleAsync`, `RegisterAsync` |
| Create | `pkg/tool/delegate/delegate_test.go` | Unit tests for `buildTaskNotification` and `handleAsync` |
| Modify | `cmd/hermes/main.go` | Register async tool; replace blocking REPL with `select` loop |

---

### Task 1: AIAgent async notification infrastructure

**Files:**
- Modify: `pkg/agent/agent.go`
- Create: `pkg/agent/async_notif_test.go`

- [ ] **Step 1.1: Write failing tests**

Create `pkg/agent/async_notif_test.go`:

```go
package agent

import (
	"context"
	"testing"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchrunner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func TestAddNotification_StoresAndSignals(t *testing.T) {
	a := &AIAgent{notifCh: make(chan struct{}, 1)}

	a.AddNotification("<task-notification><task-id>t1</task-id></task-notification>")

	if len(a.pendingNotifs) != 1 {
		t.Fatalf("expected 1 pending notif, got %d", len(a.pendingNotifs))
	}
	select {
	case <-a.notifCh:
	case <-time.After(10 * time.Millisecond):
		t.Fatal("expected signal on notifCh, got timeout")
	}
}

func TestAddNotification_MultipleNotifsSingleSignal(t *testing.T) {
	a := &AIAgent{notifCh: make(chan struct{}, 1)}

	a.AddNotification("notif1")
	a.AddNotification("notif2")
	a.AddNotification("notif3")

	if len(a.pendingNotifs) != 3 {
		t.Fatalf("expected 3 notifs, got %d", len(a.pendingNotifs))
	}
	count := 0
	for {
		select {
		case <-a.notifCh:
			count++
		default:
			if count != 1 {
				t.Fatalf("expected 1 signal, got %d", count)
			}
			return
		}
	}
}

func TestNotifCh_ReturnsReadOnly(t *testing.T) {
	a := &AIAgent{notifCh: make(chan struct{}, 1)}
	if a.NotifCh() == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestRun_GuardEmptyInputNoPendingNotifs(t *testing.T) {
	cfg := types.AgentConfig{Model: "test/model", WorkDir: t.TempDir(), MaxDelegateDepth: 2}
	a := NewAIAgent(cfg, nil, nil)
	// Pre-populate messages to skip system-prompt init path
	a.messages = []types.Message{{Role: "system", Content: "sys"}}

	reply, pending, err := a.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pending || reply != "" {
		t.Fatalf("expected empty reply and pending=false, got reply=%q pending=%v", reply, pending)
	}
}

func TestRun_DrainsPendingNotificationsAsMessages(t *testing.T) {
	// Wire mock LLM so Run() completes successfully
	entry, ok := orchestrator.LookupNodeType("llm")
	if !ok {
		t.Fatal("llm node type not registered")
	}
	r, ok := entry.Runner.(*orchrunner.LLMRunner)
	if !ok {
		t.Fatal("llm runner not found")
	}
	r.SetInvoker(&mockLLMInvoker{reply: "ack"})

	cfg := types.AgentConfig{Model: "test/model", WorkDir: t.TempDir(), MaxDelegateDepth: 2}
	a := NewAIAgent(cfg, nil, nil)
	a.messages = []types.Message{{Role: "system", Content: "sys"}}

	notif := "<task-notification><task-id>abc</task-id><status>completed</status><result>done</result></task-notification>"
	a.pendingNotifs = []string{notif}

	a.Run(context.Background(), "")

	a.mu.Lock()
	msgs := make([]types.Message, len(a.messages))
	copy(msgs, a.messages)
	a.mu.Unlock()

	found := false
	for _, m := range msgs {
		if m.Role == string(types.RoleUser) && m.Content == notif {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("notification not injected into messages; got %d messages", len(msgs))
	}
}
```

- [ ] **Step 1.2: Run tests to verify they fail**

```
go test ./pkg/agent/... -run "TestAddNotification|TestNotifCh|TestRun_Guard|TestRun_Drains" -v
```

Expected: compile error — `AddNotification`, `NotifCh`, `pendingNotifs` not defined.

- [ ] **Step 1.3: Add fields to AIAgent struct**

In `pkg/agent/agent.go`, add two fields to the `AIAgent` struct after the `eventCB` field:

```go
	// 事件回调
	eventCB EventCallback

	// 异步子 Agent 通知
	pendingNotifs []string      // 子 agent 完成通知队列，受 mu 保护
	notifCh       chan struct{}  // 信号 channel，buffered(1)
```

- [ ] **Step 1.4: Initialize notifCh in NewAIAgent**

In `NewAIAgent`, inside the `a := &AIAgent{...}` literal, add:

```go
	a := &AIAgent{
		config:        cfg,
		router:        router,
		registry:      reg,
		graph:         graph,
		executor:      orchexec.NewExecutor(nil),
		promptBuilder: pb,
		convMem:       &orchcontext.ConversationMemory{SessionID: cfg.SessionID},
		stats:         Stats{StartTime: time.Now()},
		notifCh:       make(chan struct{}, 1),
	}
```

- [ ] **Step 1.5: Add AddNotification and NotifCh methods**

Add after the `emitEvent` method in `pkg/agent/agent.go`:

```go
// AddNotification enqueues an async child-agent completion notification.
// Safe to call from multiple goroutines.
func (a *AIAgent) AddNotification(xml string) {
	a.mu.Lock()
	a.pendingNotifs = append(a.pendingNotifs, xml)
	a.mu.Unlock()
	select {
	case a.notifCh <- struct{}{}:
	default: // channel already has a pending signal; skip
	}
}

// NotifCh returns a read-only channel that receives a signal whenever a new
// async notification is enqueued. Consumers should call Run(ctx, "") to process it.
func (a *AIAgent) NotifCh() <-chan struct{} { return a.notifCh }
```

- [ ] **Step 1.6: Update Run() to drain notifications and add guard**

In `pkg/agent/agent.go`, replace the `Run` method's lock section. Find this block (around line 244):

```go
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
```

Replace with:

```go
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

	// Drain pending async notifications, injecting each as a user message
	notifsDrained := len(a.pendingNotifs)
	for _, notif := range a.pendingNotifs {
		a.messages = append(a.messages, types.Message{
			Role:      types.RoleUser,
			Content:   notif,
			Timestamp: time.Now(),
		})
	}
	a.pendingNotifs = nil

	// Append real user message (only when non-empty)
	if userInput != "" {
		a.messages = append(a.messages, types.Message{
			Role:      types.RoleUser,
			Content:   userInput,
			Timestamp: time.Now(),
		})
		a.turnNum++
	}
	a.mu.Unlock()

	// Guard: nothing new to process — notifCh was signaled but notifications
	// were already consumed by a concurrent Run call.
	if userInput == "" && notifsDrained == 0 {
		return "", false, nil
	}
```

- [ ] **Step 1.7: Run tests to verify they pass**

```
go test ./pkg/agent/... -run "TestAddNotification|TestNotifCh|TestRun_Guard|TestRun_Drains" -v
```

Expected: all 5 tests PASS.

- [ ] **Step 1.8: Run full agent test suite to check for regressions**

```
go test ./pkg/agent/... -v
```

Expected: all tests PASS.

- [ ] **Step 1.9: Commit**

```
git add pkg/agent/agent.go pkg/agent/async_notif_test.go
git commit -m "feat(agent): add async notification infrastructure for child agents"
```

---

### Task 2: Extract shared helpers and add buildTaskNotification

**Files:**
- Modify: `pkg/tool/delegate/delegate.go`
- Create: `pkg/tool/delegate/delegate_test.go`

- [ ] **Step 2.1: Write failing test for buildTaskNotification**

Create `pkg/tool/delegate/delegate_test.go`:

```go
package delegate

import (
	"strings"
	"testing"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
)

func TestBuildFullTask_TaskOnly(t *testing.T) {
	args := delegateArgs{Task: "do the thing"}
	got := buildFullTask(args)
	if got != "do the thing" {
		t.Fatalf("expected %q, got %q", "do the thing", got)
	}
}

func TestBuildFullTask_WithContextAndConstraints(t *testing.T) {
	args := delegateArgs{
		Task:        "do the thing",
		Context:     "file: main.go",
		Constraints: "no side effects",
	}
	got := buildFullTask(args)
	if !strings.Contains(got, "## Context\nfile: main.go") {
		t.Errorf("missing Context section: %s", got)
	}
	if !strings.Contains(got, "## Constraints\nno side effects") {
		t.Errorf("missing Constraints section: %s", got)
	}
}

func TestBuildTaskNotification_Completed(t *testing.T) {
	stats := agent.Stats{TotalIterations: 3, ToolCalls: 7}
	xml := buildTaskNotification("task-123", "analyze auth", "found bug at line 42", nil, 5*time.Second, stats)

	for _, want := range []string{
		"<task-notification>",
		"<task-id>task-123</task-id>",
		"<status>completed</status>",
		"<task>analyze auth</task>",
		"<iterations>3</iterations>",
		"<tool-calls>7</tool-calls>",
		"found bug at line 42",
		"</task-notification>",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("missing %q in output:\n%s", want, xml)
		}
	}
}

func TestBuildTaskNotification_Failed(t *testing.T) {
	stats := agent.Stats{}
	xml := buildTaskNotification("task-456", "fix bug", "", fmt.Errorf("timeout"), time.Second, stats)

	if !strings.Contains(xml, "<status>failed</status>") {
		t.Errorf("expected failed status:\n%s", xml)
	}
	if !strings.Contains(xml, "timeout") {
		t.Errorf("expected error in result:\n%s", xml)
	}
}
```

Add `"fmt"` to imports in the test file.

- [ ] **Step 2.2: Run tests to verify they fail**

```
go test ./pkg/tool/delegate/... -v
```

Expected: compile error — `buildFullTask`, `buildTaskNotification` not defined.

- [ ] **Step 2.3: Extract buildFullTask and add buildTaskNotification**

In `pkg/tool/delegate/delegate.go`:

1. Add `"fmt"` to imports if not present.

2. Add these two functions before the `handle` method:

```go
// buildFullTask assembles the complete task prompt from delegate arguments.
func buildFullTask(args delegateArgs) string {
	fullTask := args.Task
	if args.Context != "" {
		fullTask += "\n\n## Context\n" + args.Context
	}
	if args.Constraints != "" {
		fullTask += "\n\n## Constraints\n" + args.Constraints
	}
	return fullTask
}

// buildTaskNotification formats a completed (or failed) child-agent result as
// an XML <task-notification> block that the parent LLM can parse.
func buildTaskNotification(taskID, task, result string, err error, elapsed time.Duration, stats agent.Stats) string {
	status := "completed"
	if err != nil {
		status = "failed"
		result = err.Error()
	}
	return fmt.Sprintf(`<task-notification>
<task-id>%s</task-id>
<status>%s</status>
<task>%s</task>
<elapsed>%s</elapsed>
<iterations>%d</iterations>
<tool-calls>%d</tool-calls>
<result>%s</result>
</task-notification>`,
		taskID, status, task,
		elapsed.Round(time.Millisecond).String(),
		stats.TotalIterations,
		stats.ToolCalls,
		result,
	)
}
```

3. Update `handle` to use `buildFullTask`. Replace these lines inside `handle`:

```go
	// 组装完整的任务描述
	fullTask := args.Task
	if args.Context != "" {
		fullTask += "\n\n## Context\n" + args.Context
	}
	if args.Constraints != "" {
		fullTask += "\n\n## Constraints\n" + args.Constraints
	}
```

With:

```go
	fullTask := buildFullTask(args)
```

4. Add `"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"` to imports in `delegate.go` (needed for `agent.Stats`).

- [ ] **Step 2.4: Run tests to verify they pass**

```
go test ./pkg/tool/delegate/... -v
```

Expected: all 4 tests PASS.

- [ ] **Step 2.5: Commit**

```
git add pkg/tool/delegate/delegate.go pkg/tool/delegate/delegate_test.go
git commit -m "feat(delegate): extract buildFullTask, add buildTaskNotification helper"
```

---

### Task 3: delegate_task_async tool

**Files:**
- Modify: `pkg/tool/delegate/delegate.go`
- Modify: `pkg/tool/delegate/delegate_test.go`
- Modify: `pkg/agent/agent.go` (disable async tool in child agents)
- Modify: `cmd/hermes/main.go` (register async tool)

- [ ] **Step 3.1: Write failing tests for handleAsync**

Append to `pkg/tool/delegate/delegate_test.go`:

```go
func TestHandleAsync_ReturnsImmediately(t *testing.T) {
	cfg := types.AgentConfig{
		Model:                 "test/model",
		WorkDir:               t.TempDir(),
		MaxDelegateDepth:      2,
		DelegateMaxIterations: 5,
	}
	parentAgent := agent.NewAIAgent(cfg, nil, nil)
	p := NewProvider(parentAgent, 3)

	args := json.RawMessage(`{"task": "analyze something"}`)

	start := time.Now()
	result, err := p.handleAsync(context.Background(), args)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("handleAsync error: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("handleAsync blocked for %v (expected < 500ms)", elapsed)
	}
	if !strings.Contains(result, "task-") {
		t.Errorf("expected task ID in result, got: %s", result)
	}
	if !strings.Contains(result, "background") {
		t.Errorf("expected 'background' in result, got: %s", result)
	}
}

func TestHandleAsync_NotifiesParentOnCompletion(t *testing.T) {
	cfg := types.AgentConfig{
		Model:                 "test/model",
		WorkDir:               t.TempDir(),
		MaxDelegateDepth:      2,
		DelegateMaxIterations: 3,
	}
	parentAgent := agent.NewAIAgent(cfg, nil, nil)
	p := NewProvider(parentAgent, 3)

	args := json.RawMessage(`{"task": "quick task"}`)
	_, err := p.handleAsync(context.Background(), args)
	if err != nil {
		t.Fatalf("handleAsync error: %v", err)
	}

	// Child will fail fast (no LLM), but must still send a notification
	select {
	case <-parentAgent.NotifCh():
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("parent did not receive notification within 5s")
	}
}
```

Add required imports to `delegate_test.go`:

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)
```

- [ ] **Step 3.2: Run tests to verify they fail**

```
go test ./pkg/tool/delegate/... -run "TestHandleAsync" -v
```

Expected: compile error — `handleAsync` not defined.

- [ ] **Step 3.3: Add handleAsync and RegisterAsync to delegate.go**

Add these imports to `delegate.go` if missing: `"context"`, `"encoding/json"`.

Append to `pkg/tool/delegate/delegate.go`:

```go
// handleAsync executes a child agent in a background goroutine and returns
// immediately with a task ID. The child's result is delivered via AddNotification.
func (p *Provider) handleAsync(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args delegateArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Task == "" {
		return "", fmt.Errorf("task description is required")
	}

	taskID := fmt.Sprintf("task-%d", time.Now().UnixMilli())

	child, err := p.parentAgent.NewChildAgent(args.Task)
	if err != nil {
		return "", fmt.Errorf("create child agent: %w", err)
	}

	fullTask := buildFullTask(args)

	log.Info("dispatching async child agent",
		"task_id", taskID,
		"task_preview", truncate(args.Task, 100),
		"depth", child.Depth(),
	)

	go func() {
		start := time.Now()
		result, _, runErr := child.Run(context.Background(), fullTask)
		stats := child.GetStats()
		xml := buildTaskNotification(taskID, args.Task, result, runErr, time.Since(start), stats)
		p.parentAgent.AddNotification(xml)
		log.Info("async child agent finished",
			"task_id", taskID,
			"elapsed", time.Since(start).Round(time.Millisecond),
			"iterations", stats.TotalIterations,
			"tool_calls", stats.ToolCalls,
		)
	}()

	return fmt.Sprintf(
		`Task "%s" started (id: %s). Running in background — you will receive a <task-notification> when complete.`,
		truncate(args.Task, 80), taskID,
	), nil
}

// RegisterAsync registers the delegate_task_async tool with the global registry.
func (p *Provider) RegisterAsync() error {
	schema := types.ToolSchema{
		Type: "function",
		Function: types.FunctionSchema{
			Name: "delegate_task_async",
			Description: `Delegate a task to a sub-agent running in the background. Returns immediately with a task ID.
The sub-agent runs independently; you will receive a <task-notification> message when it completes.

Use this when:
- The task is independent and does not block your current reasoning
- You want to run multiple sub-tasks in parallel
- The task is long-running and you can continue responding to the user meanwhile

Use delegate_task (sync) instead when you need the result before taking your next step.

Constraints:
- Max delegation depth: 2 (cannot be called by a child agent)
- Sub-agent cannot interact with the user directly`,
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"task": {
						"type": "string",
						"description": "Clear, self-contained description of what the sub-agent should accomplish"
					},
					"context": {
						"type": "string",
						"description": "Additional context: file paths, variable values, background info the sub-agent needs"
					},
					"constraints": {
						"type": "string",
						"description": "Specific constraints or requirements: output format, scope boundaries"
					}
				},
				"required": ["task"],
				"additionalProperties": false
			}`),
		},
	}

	return registry.Register(&registry.ToolEntry{
		Name:          "delegate_task_async",
		Toolset:       "agent",
		Schema:        schema,
		Handler:       p.handleAsync,
		ParallelSafe:  true,
		NeverParallel: false,
		MaxResultSize: 200,
	})
}
```

- [ ] **Step 3.4: Run tests to verify they pass**

```
go test ./pkg/tool/delegate/... -v
```

Expected: all 6 tests PASS (the 2 new tests + 4 from Task 2).

- [ ] **Step 3.5: Disable delegate_task_async in child agents**

In `pkg/agent/agent.go`, find `NewChildAgent` and update `DisabledTools`:

```go
	childCfg.DisabledTools = append(append([]string{}, childCfg.DisabledTools...),
		"delegate_task",       // 防递归
		"delegate_task_async", // 防递归 (async variant)
		"clarify",             // 无交互
		"memory",              // 防共享写入
	)
```

- [ ] **Step 3.6: Register async tool in main.go**

In `cmd/hermes/main.go`, find the `delegateProvider.Register()` block and add `RegisterAsync`:

```go
	// ── 注册 delegate_task 工具 ──
	delegateProvider := delegate.NewProvider(ag, 3)
	if err := delegateProvider.Register(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  delegate_task register: %v\n", err)
	}
	if err := delegateProvider.RegisterAsync(); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  delegate_task_async register: %v\n", err)
	}
```

- [ ] **Step 3.7: Run full test suite**

```
go test ./... -v 2>&1 | tail -30
```

Expected: all tests PASS, no compilation errors.

- [ ] **Step 3.8: Commit**

```
git add pkg/tool/delegate/delegate.go pkg/tool/delegate/delegate_test.go pkg/agent/agent.go cmd/hermes/main.go
git commit -m "feat(delegate): add delegate_task_async tool with goroutine execution and XML notification"
```

---

### Task 4: REPL select-based loop

**Files:**
- Modify: `cmd/hermes/main.go`

- [ ] **Step 4.1: Extract command handler helper**

In `cmd/hermes/main.go`, add this function before `persistMessages`:

```go
// handleReplCommand processes /commands. Returns true if the input was a command.
func handleReplCommand(
	input string,
	ag *agent.AIAgent,
	todoStore *builtin.TodoStore,
	mcpMgr *mcp.Manager,
	sessionDB *state.SessionDB,
	sessionID string,
) bool {
	switch input {
	case "/quit", "/exit":
		if mcpMgr != nil {
			mcpMgr.ShutdownAll()
		}
		persistMessages(sessionDB, sessionID, ag)
		ag.Shutdown()
		if sessionDB != nil {
			sessionDB.Close()
		}
		fmt.Println("👋 Bye!")
		os.Exit(0)
	case "/stats":
		stats := ag.GetStats()
		fmt.Printf("  Iterations: %d | Tool calls: %d | Tokens: %d in / %d out\n",
			stats.TotalIterations, stats.ToolCalls, stats.InputTokens, stats.OutputTokens)
	case "/budget":
		fmt.Println("  Budget tracking: managed by graph executor (MaxSteps = cfg.MaxIterations)")
	case "/todo":
		items := todoStore.Read()
		if len(items) == 0 {
			fmt.Println("  (no TODO list)")
		} else {
			fmt.Println(todoStore.Summary())
		}
	case "/mcp":
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
	default:
		return false
	}
	return true
}
```

- [ ] **Step 4.2: Replace blocking scanner REPL with select loop**

In `cmd/hermes/main.go`, find the REPL section that starts with:

```go
	// REPL 循环
	fmt.Println("🏛️  Hermes Agent (Go) — type /quit to exit")
```

And ends with the closing `}` of `main`. Replace the entire scanner loop (everything from `scanner := bufio.NewScanner(os.Stdin)` to the end of the REPL `for` block) with:

```go
	// REPL 循环
	fmt.Println("🏛️  Hermes Agent (Go) — type /quit to exit")
	fmt.Printf("   Model: %s | Budget: %d iterations\n", cfg.Model, cfg.MaxIterations)
	fmt.Printf("   Session: %s\n", sessionID)
	fmt.Printf("   Memory: %s/memories\n\n", hermesHome)

	stdinCh := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			stdinCh <- scanner.Text()
		}
		close(stdinCh)
	}()

	fmt.Print(">>> ")
	for {
		select {
		case line, ok := <-stdinCh:
			if !ok {
				return // stdin closed (e.g. piped input exhausted)
			}
			input := strings.TrimSpace(line)
			if input == "" {
				fmt.Print(">>> ")
				continue
			}
			if handleReplCommand(input, ag, todoStore, mcpMgr, sessionDB, sessionID) {
				fmt.Print(">>> ")
				continue
			}

			streamedThisTurn = false
			reply, _, err := ag.Run(ctx, input)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n❌ Error: %v\n", err)
			} else if !streamedThisTurn && reply != "" {
				fmt.Println(reply)
			} else {
				fmt.Println()
			}
			fmt.Print(">>> ")

		case <-ag.NotifCh():
			// An async child agent completed — process its notification.
			streamedThisTurn = false
			reply, _, err := ag.Run(ctx, "")
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[async] ❌ %v\n", err)
			} else if reply != "" {
				if !streamedThisTurn {
					fmt.Printf("\n[async] %s\n", reply)
				} else {
					fmt.Println()
				}
			}
			fmt.Print(">>> ")
		}
	}
```

- [ ] **Step 4.3: Build to verify compilation**

```
go build ./cmd/hermes/...
```

Expected: no errors.

- [ ] **Step 4.4: Manual smoke test — sync delegate still works**

```
go run ./cmd/hermes/...
```

At the `>>>` prompt, enter:
```
say hello
```
Expected: normal reply, no regression in sync behavior.

- [ ] **Step 4.5: Manual smoke test — async delegate (if LLM configured)**

If a real LLM is configured via `HERMES_CONFIG` or env, enter:
```
delegate_task_async: summarize what delegate_task_async does based on what you know
```
Or instruct the agent with a prompt that triggers `delegate_task_async`.

Expected:
1. Agent replies immediately with "Task xxx started... background"
2. Prompt returns to `>>>` without blocking
3. Within seconds, `[async]` prefix appears with the child's result

- [ ] **Step 4.6: Commit**

```
git add cmd/hermes/main.go
git commit -m "feat(repl): replace blocking scanner with select loop for async agent notifications"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task covering it |
|-----------------|-----------------|
| `pendingNotifs []string` + `notifCh chan struct{}` fields | Task 1 |
| `AddNotification()` thread-safe | Task 1 |
| `NotifCh()` read-only channel | Task 1 |
| `Run()` drain notifications as user messages | Task 1 |
| Guard: empty input + no notifs → return early | Task 1 |
| `buildFullTask` shared helper | Task 2 |
| `buildTaskNotification` XML format | Task 2 |
| `handleAsync` spawns goroutine, returns immediately | Task 3 |
| `RegisterAsync` with correct tool schema | Task 3 |
| Child agents cannot call `delegate_task_async` | Task 3 |
| Register async tool in main.go | Task 3 |
| REPL `select` on stdin + notifCh | Task 4 |
| `[async]` prefix on notification-triggered output | Task 4 |

**Type consistency:**
- `agent.Stats` used in `buildTaskNotification` — exported in `pkg/agent/agent.go` ✓
- `AddNotification(xml string)` called in `handleAsync` — matches definition ✓
- `NotifCh() <-chan struct{}` matches `<-ag.NotifCh()` in REPL ✓
- `buildFullTask(args delegateArgs)` used in both `handle` and `handleAsync` ✓

**No placeholders:** All code blocks are complete and runnable. ✓
