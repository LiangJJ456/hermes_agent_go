# Async Delegate Agent — 设计文档

> 创建时间：2026-05-27
> 分支：feat/graph-orchestrator

---

## 背景

当前 `delegate_task` 工具是同步阻塞的：父 agent 派出子 agent 后，父 agent 的整个 `Run()` 调用会阻塞，直到子 agent 跑完才继续。这在子任务耗时较长时会占用父 agent 的全部执行权，用户无法与父 agent 继续交互。

参考 Claude Code 源码的 Multi-Agent 机制（消息队列 + XML 通知模式），本文档设计一套轻量异步委托方案。

---

## 目标

- 父 agent 派出子 agent 后**立即继续**，不阻塞当前 turn
- LLM **动态决定**用同步还是异步委托（工具选择），图结构不写死
- 子 agent 完成后，结果**自动注入**父 agent 下一轮对话
- 改动范围最小，不引入外部依赖

## 非目标

- 跨进程 / 持久化消息队列（单进程内已足够）
- Fork subagent / prompt cache 优化（独立议题）
- Coordinator 模式（后续再做）

---

## 方案：3A 双工具 + 通知注入

### 核心思路

暴露两个工具给 LLM：

| 工具 | 行为 | 适用场景 |
|------|------|---------|
| `delegate_task` | 同步阻塞，直接返回结果 | 需要结果才能继续下一步 |
| `delegate_task_async` | 立即返回 task ID，后台跑 | 独立任务、可并行、不阻塞当前对话 |

图结构**不变**，两个工具都走现有的通用 `tool` 节点，LLM 的 tool_call 选择即为决策点。

### 数据流

```
父 LLM 输出 tool_call: "delegate_task_async"
    ↓
tool 节点 → handleAsync()
    ↓
goroutine 启动，立即返回 "Task abc started"
    ↓
父 LLM 继续（本轮可做其他事 / 回复用户）
    ↓
[后台] child.Run() 完成
    ↓
parent.AddNotification(xml)  →  pendingNotifs append + notifCh signal
    ↓
REPL select 触发 → agent.Run(ctx, "")
    ↓
Run() 入口 drain pendingNotifs → 注入为 user 消息 → 正常跑图
    ↓
父 LLM 看到 <task-notification>，合成最终结果
```

---

## 组件设计

### 1. AIAgent（`pkg/agent/agent.go`）

新增字段：

```go
type AIAgent struct {
    // ...现有字段不变...

    pendingNotifs []string      // 子 agent 完成通知队列，受 mu 保护
    notifCh       chan struct{}  // 信号 channel，buffered(1)，REPL 监听用
}
```

构造时初始化：

```go
a := &AIAgent{
    // ...
    notifCh: make(chan struct{}, 1),
}
```

新增方法：

```go
// AddNotification 供子 agent goroutine 调用（线程安全）
func (a *AIAgent) AddNotification(xml string) {
    a.mu.Lock()
    a.pendingNotifs = append(a.pendingNotifs, xml)
    a.mu.Unlock()
    select {
    case a.notifCh <- struct{}{}: // 非阻塞，已有信号则跳过
    default:
    }
}

// NotifCh 返回只读信号 channel，供 REPL 监听
func (a *AIAgent) NotifCh() <-chan struct{} { return a.notifCh }
```

`Run()` 开头加 drain 逻辑（在 `a.mu.Lock()` 区块内）：

```go
func (a *AIAgent) Run(ctx context.Context, userInput string) (string, bool, error) {
    a.mu.Lock()

    // 初始化 system prompt（首轮）
    if len(a.messages) == 0 { /* ... */ }

    // drain 挂起的异步通知，注入为 user 消息
    notifsDrained := len(a.pendingNotifs)
    for _, notif := range a.pendingNotifs {
        a.messages = append(a.messages, types.Message{
            Role:      types.RoleUser,
            Content:   notif,
            Timestamp: time.Now(),
        })
    }
    a.pendingNotifs = nil

    // 追加真实用户消息（可为空，仅处理通知时）
    if userInput != "" {
        a.messages = append(a.messages, types.Message{
            Role:      types.RoleUser,
            Content:   userInput,
            Timestamp: time.Now(),
        })
        a.turnNum++
    }
    a.mu.Unlock()

    // Guard：既无用户输入也无通知（notifCh 被重复消费），直接返回
    if userInput == "" && notifsDrained == 0 {
        return "", false, nil
    }

    // ...其余逻辑不变
}
```

### 2. delegate_task_async 工具（`pkg/tool/delegate/delegate.go`）

新增注册方法 `RegisterAsync()`，处理函数 `handleAsync()`：

```go
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

    fullTask := buildFullTask(args) // 从现有 handle() 提取的共享函数：拼接 task + context + constraints

    // 用独立 context，防止父 ctx cancel 误杀子 agent
    childCtx := context.Background()

    go func() {
        start := time.Now()
        result, _, err := child.Run(childCtx, fullTask)
        stats := child.GetStats()
        xml := buildTaskNotification(taskID, args.Task, result, err, time.Since(start), stats)
        p.parentAgent.AddNotification(xml)
        log.Info("async child agent completed", "task_id", taskID,
            "elapsed", time.Since(start), "iterations", stats.TotalIterations)
    }()

    return fmt.Sprintf(
        `Task "%s" started (id: %s). Running in background — you will receive a <task-notification> when complete.`,
        truncate(args.Task, 80), taskID,
    ), nil
}
```

XML 通知格式：

```xml
<task-notification>
  <task-id>task-1748001234567</task-id>
  <status>completed</status>
  <task>分析 auth 模块的权限漏洞</task>
  <elapsed>12.3s</elapsed>
  <iterations>8</iterations>
  <tool-calls>15</tool-calls>
  <result>发现 validate.ts:42 存在空指针...</result>
</task-notification>
```

失败时 `<status>failed</status>`，`<result>` 填错误信息。

### 3. REPL（`cmd/hermes/main.go`）

将现有阻塞式 scanner 改为 select 多路复用：

```go
stdinCh := make(chan string, 1)
go func() {
    scanner := bufio.NewScanner(os.Stdin)
    for scanner.Scan() {
        stdinCh <- scanner.Text()
    }
    close(stdinCh)
}()

for {
    select {
    case line, ok := <-stdinCh:
        if !ok { return } // stdin closed
        if strings.TrimSpace(line) == "" { continue }
        reply, pending, err := agent.Run(ctx, line)
        // ...处理 reply / pending / err

    case <-agent.NotifCh():
        // 子 agent 完成，自动触发父 agent 处理通知
        reply, _, err := agent.Run(ctx, "")
        if err != nil {
            fmt.Fprintf(os.Stderr, "[async error] %v\n", err)
            continue
        }
        if reply != "" {
            fmt.Printf("\n[async] %s\n> ", reply)
        }
    }
}
```

### 4. System Prompt 工具说明

在 `pkg/agent/prompt/builder.go` 或工具 schema description 里区分两者：

```
delegate_task
  同步委托子 agent。子 agent 结果直接作为工具返回值，你拿到结果后才继续。
  用于：下一步依赖本任务结果的场景（如「先分析再实现」）。

delegate_task_async
  异步委托子 agent。立即返回，子 agent 在后台独立运行。
  完成后你会收到一条 <task-notification> 消息，再做合成。
  用于：独立任务、可并行的任务、不影响当前对话流的长耗时任务。
```

---

## 边界情况与错误处理

| 场景 | 处理方式 |
|------|---------|
| 子 agent 运行出错 | goroutine 捕获 err，发送 `<status>failed</status>` 通知，父 agent 知悉后决定是否重试 |
| 父 agent ctx 被取消 | 子 agent 用独立 `context.Background()`，不受影响；父 agent 退出后通知无人消费（可接受） |
| 多个子 agent 同时完成 | `pendingNotifs` 被 mu 保护，notifCh 是 buffered(1) 信号，REPL 下次 select 时一次性 drain 所有通知 |
| depth 超限 | `NewChildAgent` 返回 `ErrDelegateDepthExceeded`，handleAsync 返回错误，LLM 看到工具报错 |
| Run(ctx, "") 且无通知 | pendingNotifs 为空，messages 不变，不追加空消息，图正常跑（等同 no-op） |

---

## 改动范围

| 文件 | 改动 |
|------|------|
| `pkg/agent/agent.go` | 新增 `pendingNotifs`, `notifCh` 字段；新增 `AddNotification()`, `NotifCh()`；`Run()` 加 drain 逻辑 |
| `pkg/tool/delegate/delegate.go` | 新增 `handleAsync()`, `RegisterAsync()`, `buildTaskNotification()` |
| `cmd/hermes/main.go` | REPL 改为 select 多路复用 |
| `pkg/agent/prompt/builder.go` | 工具说明区分同步/异步语义（可选，也可在 schema description 里写） |

约 **150-200 行**新增代码，无新依赖，不改动 orchestrator 层。

---

## 与 Claude Code 设计的对比

| 维度 | Claude Code | hermes_agent_go (本方案) |
|------|------------|--------------------------|
| 通信模型 | 消息队列 + React state | Go channel + mutex slice |
| 子→父通知格式 | XML `<task-notification>` | XML `<task-notification>`（相同） |
| 注入方式 | 伪装为 user 消息 | 同上 |
| 触发机制 | JS event loop 自动 | REPL select 自动 |
| 持久化 | 落盘，可跨进程恢复 | 内存，单进程 |
| LLM 决策 | 工具选择 | 工具选择（相同） |
