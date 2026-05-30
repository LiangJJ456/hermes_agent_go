# Runner State Decoupling 设计 (per-execution services)

**日期**: 2026-05-31
**分支**: feat/graph-orchestrator
**关联**: 全局单例 runner 隐患(见 `pkg/agent/agent.go` `wireRunners` 上的 `TODO(tech-debt)`);确定性 bug —— 首次委托后父 agent 永久丢失 delegate 工具。

## 背景与问题

orchestrator 的节点 runner 通过 `orchestrator.RegisterNodeType` 注册为**进程级全局单例**(每种类型一个实例)。这些 runner 持有 per-agent 可变状态:

- `LLMRunner{ Invoker, OnStreamDelta }`
- `ToolRunner{ Invoker, OnToolStart }`
- `ParallelRunner{ executor }`

`AIAgent.wireRunners()` 在每个 agent 构造时把这些字段**改写**为"当前 agent"的 invoker / tracer / executor。由于 runner 是全局共享的,**最后一个调用 wireRunners 的 agent 全局获胜**。

**已确认的后果:**

1. **确定性功能 bug**:LLM 每回合的工具列表由 `RouterAdapter.GetSchemas(a.Config.DisabledTools)` 构建(`pkg/agent/adapters/llm_invoker.go:48`),而 `LLMRunner.Invoker` 是全局的。一次委托后,全局 `LLMRunner.Invoker` 残留指向**子 agent 的 RouterAdapter**(子 agent 禁用了 `delegate_task`/`delegate_task_async` 防递归)。此后父 agent 的 LLM 请求**不再被提供 delegate 工具** → "起异步任务"只有首次有效,之后静默退化为直接工具调用。回归测试 `pkg/agent/delegate_invoker_leak_test.go` 已确定性复现。
2. **data race**:异步委托时,子 agent 在后台 goroutine 跑、父 agent 同时继续,两者并发改写/读取同一组全局 runner 字段。
3. **显示泄漏**(已用 124e632 / 6591038 打补丁):全局 `ToolRunner.OnToolStart` 残留指向子 agent 的 tracer,曾吞掉父 agent 的 `🔧` 标记。

根因唯一:**runner 持有 per-agent 可变状态,却是全局共享的**。

## 目标与非目标

**目标**
- 消除 runner 上的 per-agent 可变状态,使并发/异步委托无 race、父 agent 委托后不丢失工具与 invoker。
- 保持现有节点注册模型(runner 仍是全局单例,但变为**无状态**)。

**非目标(YAGNI)**
- 不引入工厂模式 / per-execution runner 实例。
- 不改 graph / condition 求值。
- 不改 ConvMem、记忆、skill 等无关路径。

## 核心决策

| 决策点 | 结论 |
|---|---|
| 方案 | 无状态 runner + per-agent Executor 携带服务,经 `ExecutionContext` 注入 |
| 服务契约的家 | `pkg/orchestrator/context` 包(基座,runner/adapters 可 import,无环) |
| runner 读服务的方式 | 复用现有的 `execCtx.(*agcontext.ExecutionContext)` type-assert(它们已用此读 ConvMem) |
| 函数字段 `OnStreamDelta`/`OnToolStart` | 删除 —— runner 直接调 `ec.Tracer` |
| 向后兼容 | `runner` 保留接口类型别名,adapters 等零改动 |

`context` import `trace` 已确认无环(`trace` 仅 import `pkg/log`)。

## 架构

### `context` 包 — 服务契约

新增 `pkg/orchestrator/context/services.go`,从 `runner` 迁入服务接口**及其牵连的类型**。
因为 `LLMInvoker` 的签名引用 `LLMMessage`/`LLMConfig`,这两个类型一并迁入(否则 `context`
需 import `runner` → 成环)。`context` 新增 import `orchestrator`(取 `NodeResult`/`Graph`/
`ExecutionSnapshot`)和 `trace`(Tracer 字段);二者均不反向 import `context`,无环。

迁入 `context` 的完整集合:
- 接口:`LLMInvoker`、`ToolInvoker`、`GraphExecutor`、`ContextGraphExecutor`
- 类型:`LLMMessage`、`LLMConfig`(被 `LLMInvoker` 引用)

```go
// 签名与现 runner 版本逐字一致：
type LLMConfig struct { /* Model/SystemPrompt/UserPrompt/Tools/OutputSchema/Temperature/MaxTokens */ }
type LLMMessage struct { /* Role/Content/Name/ToolCalls/ToolCallID */ }

type LLMInvoker interface {
	Chat(ctx context.Context, model string, messages []LLMMessage, tools []string, cfg LLMConfig) (*orchestrator.NodeResult, error)
	ChatStream(ctx context.Context, model string, messages []LLMMessage, tools []string, cfg LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error)
}
type ToolInvoker interface {
	Invoke(ctx context.Context, resource string, input interface{}, timeout uint) (*orchestrator.NodeResult, error)
}
type GraphExecutor interface {
	Execute(ctx context.Context, g *orchestrator.Graph, input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error)
}
type ContextGraphExecutor interface {
	GraphExecutor
	ExecuteWithContext(ctx context.Context, g *orchestrator.Graph, ec *ExecutionContext) (interface{}, *orchestrator.ExecutionSnapshot, error)
}
```

> `StreamDeltaFunc`/`ToolStartFunc` 不迁移 —— 它们随 runner 函数字段一并删除(runner 直接调 `ec.Tracer`)。

`ExecutionContext`(`execution.go`)新增服务字段:

```go
type ExecutionContext struct {
	WorkMem        *WorkingMemory
	ConvMem        *ConversationMemory
	TraceID        string
	CurrentSpanID  string
	DefinitionName string

	// 执行期服务(由 Executor 盖章；runner 从这里读，不再持有自己的副本）
	LLMInvoker  LLMInvoker
	ToolInvoker ToolInvoker
	Executor    GraphExecutor
	Tracer      trace.Tracer
}
```

`runner` 包保留别名以最小化改动(`graph_builder.go` 等对 `runner.LLMConfig` 的引用、
`RegisterNodeType("llm", ..., &LLMConfig{})` 均经别名透明工作):

```go
type LLMInvoker = agcontext.LLMInvoker
type ToolInvoker = agcontext.ToolInvoker
type GraphExecutor = agcontext.GraphExecutor
type ContextGraphExecutor = agcontext.ContextGraphExecutor
type LLMMessage = agcontext.LLMMessage
type LLMConfig = agcontext.LLMConfig
```

### runner 无状态化

删除 `LLMRunner{Invoker,OnStreamDelta}`、`ToolRunner{Invoker,OnToolStart}`、`ParallelRunner{executor}` 字段以及 `SetInvoker`/`SetExecutor` 方法。`Run` 改从 ec 读:

- **LLM**:`inv := ec.LLMInvoker`(nil → "no invoker configured" 错误);流式分支改为直接调 `ec.Tracer.OnStreamDelta(ctx, delta)`(`ec.Tracer != nil` 时)。
- **Tool**:`inv := ec.ToolInvoker`;`Invoker.Invoke` 前改为 `if ec.Tracer != nil { ec.Tracer.OnToolStart(ctx, resource, toolArgsStr) }`。
- **Parallel**:子 executor 取自 `ec.Executor`(替换 `r.Executor`);现有 `ContextGraphExecutor` 类型断言改在 `ec.Executor` 上。

runner 仍用现有的 `execCtx.(*agcontext.ExecutionContext)` 断言(读 ConvMem 的同一处)顺带取服务。

### Executor 携带服务并盖章

`pkg/orchestrator/executor/executor.go` 的 `Executor` 新增字段(`Tracer` 已有):

```go
type Executor struct {
	Tracer      trace.Tracer
	LLMInvoker  agcontext.LLMInvoker
	ToolInvoker agcontext.ToolInvoker
}
```

盖章点统一在 `executeFrom` 开头(`Execute`/`ExecuteWithContext`/`Resume` 三个入口都汇入 `executeFrom`),在进入节点循环前对该 ec 执行一次:

```go
ec.LLMInvoker = e.LLMInvoker
ec.ToolInvoker = e.ToolInvoker
ec.Tracer = e.Tracer
ec.Executor = e   // self，供 parallel 子图使用
```

`ParallelRunner` 跑子图时经 `ec.Executor`(= 同一个 per-agent executor)调 `ExecuteWithContext(forkedEc)` → 该子调用的 `executeFrom` 会对 `forkedEc` 重新盖同一套服务,故 fork 无需手动复制服务字段。`Fork()` 维持现状即可。

### agent 层 — 删除 wireRunners

`NewAIAgent` 不再调 `wireRunners()`;改为在构造 adapters 后设置 Executor 服务:

```go
a.executor = orchexec.NewExecutor(nil)
// ...build adapters...
a.executor.LLMInvoker = a.llmInvoker
a.executor.ToolInvoker = a.toolInvoker
```

`SetEventCallback` 仍设 `a.executor.Tracer = newEventTracer(cb, nil)`(不变)。

子 agent 的事件转发(`NewChildAgent` 中"丢 stream、标 `FromSubAgent`、转发父 cb")**保持不变**:它设的是子 agent 自己 `executor.Tracer`,子节点经 `ec.Tracer` 走子的 eventTracer → 带 `[子Agent]` 转发到父显示;父节点经父的 `ec.Tracer` 直达父 cb。全程不碰全局,故 124e632 / 6591038 的行为从"补丁"变为结构性保证,且 invoker 泄漏 bug 消失。

## 错误处理

| 场景 | 行为 |
|---|---|
| ec 为 nil(execCtx 非 ExecutionContext) | runner 取不到服务 → 现有 "no invoker configured" 错误 |
| ec 有但 invoker 未设 | 同上错误 |
| `ec.Tracer == nil` | 跳过事件发射(与 NopTracer 等价) |

## 测试计划

- **runner 单测**:`TestToolRunnerWithMockInvoker` 等改为构造带 `ToolInvoker`/`LLMInvoker` 的 `ExecutionContext` 并作为 `execCtx` 传入(替代 `SetInvoker`)。`TestToolRunnerNoInvoker`/`TestParallelRunnerNoExecutor` 仍传无服务的 ec,断言报错。
- **回归测试翻转**:
  - `delegate_invoker_leak_test.go` → 断言**委托后父仍提供 delegate 工具**(不再泄漏)。
  - `delegate_events_test.go` → 维持(子工具事件带 `FromSubAgent` 转发父 cb)。
- **新增并发测试**:并发触发父 + 多个异步子 agent,`go test -race` 必须干净。
- **全量**:`go build ./...`、`go vet`、`gofmt -l`、`go test ./...` 全绿。

## 成功标准

1. 任意次委托后父 agent 仍能发起异步委托(leak 测试翻转后通过)。
2. `go test -race` 在并发异步委托下无 race 报告。
3. 子 agent 用自己受限 invoker,父用自己的。
4. `[子Agent]` 标签与子 agent 静默仍生效。
5. 全量测试与既有行为保持绿。

## 改动文件清单

| 动作 | 文件 |
|---|---|
| 新增 | `pkg/orchestrator/context/services.go`(服务接口) |
| 改 | `pkg/orchestrator/context/execution.go`（ExecutionContext 加服务字段） |
| 改 | `pkg/orchestrator/runner/{llm,tool,parallel}.go`（无状态，读 ec，加类型别名） |
| 改 | `pkg/orchestrator/executor/executor.go`（Executor 加服务字段并盖章 ec） |
| 改 | `pkg/agent/agent.go`（删 `wireRunners`，设 `a.executor` 服务） |
| 改测试 | `pkg/orchestrator/runner/runner_test.go`、`pkg/agent/delegate_invoker_leak_test.go`、`pkg/agent/delegate_events_test.go`，新增并发 race 测试 |
