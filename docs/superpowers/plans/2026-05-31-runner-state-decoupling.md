# Runner State Decoupling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove per-agent mutable state from the process-global node runners so concurrent/async delegation is race-free and the parent agent never loses its invoker/tools after a delegation.

**Architecture:** Runners become stateless and read their services (LLMInvoker, ToolInvoker, sub-Executor, Tracer) from the per-execution `ExecutionContext` they already receive (they already read ConvMem the same way). The per-agent `Executor` carries the services and stamps them onto the `ExecutionContext` at the start of `executeFrom`. Service interfaces (plus the `LLMMessage`/`LLMConfig` they reference) move into the `context` package; `runner` keeps type aliases so external references are untouched.

**Tech Stack:** Go. Packages: `pkg/orchestrator/context` (agcontext), `pkg/orchestrator/runner`, `pkg/orchestrator/executor`, `pkg/agent`, `pkg/agent/adapters`, `pkg/trace`.

**Reference spec:** `docs/superpowers/specs/2026-05-31-runner-state-decoupling-design.md`

**Sequencing principle:** Each task leaves `go build ./...` and `go test ./...` green. Additive tasks first (new types, executor stamping), then flip each runner to read from `ec` while removing its field and its `wireRunners` block in the same task, then delete the now-empty `wireRunners`.

**Key facts (verified):**
- `context` imports nothing internal today; adding imports of `orchestrator` and `trace` introduces no cycle (`orchestrator` and `trace` do not import `context`).
- Runners already do `ec, ok := execCtx.(*agcontext.ExecutionContext)` to read ConvMem (`llm.go:73`, `tool.go:92`, `parallel.go:119`) — services are read the same way.
- `Execute`, `ExecuteWithContext`, `Resume` all funnel through `executeFrom` — stamping there covers every entry point and every forked sub-graph (parallel calls back through `ec.Executor`).
- The two regression tests already exist: `pkg/agent/delegate_invoker_leak_test.go` (currently asserts the bug) and `pkg/agent/delegate_events_test.go`.

---

## File Structure

| Action | File | Responsibility |
|---|---|---|
| Create | `pkg/orchestrator/context/services.go` | Service interfaces + `LLMMessage`/`LLMConfig` (the execution-service contract) |
| Modify | `pkg/orchestrator/context/execution.go` | Add `LLMInvoker`/`ToolInvoker`/`Executor`/`Tracer` fields to `ExecutionContext` |
| Create | `pkg/orchestrator/runner/services_alias.go` | Type aliases re-exporting the moved types from `context` |
| Modify | `pkg/orchestrator/runner/llm.go` | Delete moved defs; LLMRunner stateless (read `ec.LLMInvoker`, `ec.Tracer`) |
| Modify | `pkg/orchestrator/runner/tool.go` | Delete moved defs; ToolRunner stateless (read `ec.ToolInvoker`, `ec.Tracer`) |
| Modify | `pkg/orchestrator/runner/parallel.go` | Delete moved defs; ParallelRunner stateless (read `ec.Executor`) |
| Modify | `pkg/orchestrator/executor/executor.go` | `Executor` carries services + stamps them onto `ec` in `executeFrom` |
| Modify | `pkg/agent/agent.go` | Set `a.executor` services; delete `wireRunners` |
| Modify | `pkg/orchestrator/runner/runner_test.go` | Pass services via `ec` instead of `SetInvoker`/`SetExecutor` |
| Modify | `pkg/agent/delegate_invoker_leak_test.go` | Flip to assert the parent keeps delegate tools |
| Create | `pkg/agent/delegate_race_test.go` | `-race` concurrent-delegation test |

---

## Task 1: Service contract in the context package

**Files:**
- Create: `pkg/orchestrator/context/services.go`
- Modify: `pkg/orchestrator/context/execution.go`

- [ ] **Step 1: Create `pkg/orchestrator/context/services.go`**

```go
package context

import (
	stdctx "context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// LLMConfig configures an llm node. (Moved from runner so it can appear in the
// LLMInvoker signature without an import cycle; runner re-exports it as an alias.)
type LLMConfig struct {
	Model        string          `json:"Model"`
	SystemPrompt string          `json:"SystemPrompt,omitempty"`
	UserPrompt   string          `json:"UserPrompt,omitempty"`
	Tools        []string        `json:"Tools,omitempty"`
	OutputSchema json.RawMessage `json:"OutputSchema,omitempty"`
	Temperature  float64         `json:"Temperature,omitempty"`
	MaxTokens    int             `json:"MaxTokens,omitempty"`
}

// LLMMessage is a single message sent to the LLM.
type LLMMessage struct {
	Role       string                   `json:"Role"`
	Content    string                   `json:"Content"`
	Name       string                   `json:"Name,omitempty"`
	ToolCalls  []map[string]interface{} `json:"ToolCalls,omitempty"`
	ToolCallID string                   `json:"ToolCallID,omitempty"`
}

// LLMInvoker abstracts the actual LLM call.
type LLMInvoker interface {
	Chat(ctx stdctx.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig) (*orchestrator.NodeResult, error)
	ChatStream(ctx stdctx.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error)
}

// ToolInvoker abstracts tool execution.
type ToolInvoker interface {
	Invoke(ctx stdctx.Context, resource string, input interface{},
		timeout uint) (*orchestrator.NodeResult, error)
}

// GraphExecutor executes a sub-graph. The orchestrator Executor implements this.
type GraphExecutor interface {
	Execute(ctx stdctx.Context, g *orchestrator.Graph,
		input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error)
}

// ContextGraphExecutor extends GraphExecutor with ConvMem-aware execution.
type ContextGraphExecutor interface {
	GraphExecutor
	ExecuteWithContext(ctx stdctx.Context, g *orchestrator.Graph,
		ec *ExecutionContext) (interface{}, *orchestrator.ExecutionSnapshot, error)
}
```

NOTE: the package is named `context`; it already aliases the stdlib as needed elsewhere. Here we import the stdlib as `stdctx` to avoid the self-name clash.

- [ ] **Step 2: Add service fields to `ExecutionContext`**

In `pkg/orchestrator/context/execution.go`, add an import of `trace` and extend the struct:

```go
import (
	"code.byted.org/ad_creative/hermes_agent_go/pkg/trace"
)

// ExecutionContext holds state for a single Graph execution.
type ExecutionContext struct {
	WorkMem        *WorkingMemory
	ConvMem        *ConversationMemory
	TraceID        string
	CurrentSpanID  string
	DefinitionName string

	// Per-execution services, stamped by the running Executor. Runners read
	// these instead of holding their own (formerly global, racy) copies.
	LLMInvoker  LLMInvoker
	ToolInvoker ToolInvoker
	Executor    GraphExecutor
	Tracer      trace.Tracer
}
```

(Leave `Fork()` unchanged — sub-graphs are re-stamped by the executor that runs them.)

- [ ] **Step 3: Build to verify it compiles (additive, nothing consumes the new types yet)**

Run: `go build ./pkg/orchestrator/...`
Expected: success, no errors.

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/context/services.go pkg/orchestrator/context/execution.go
git commit -m "feat(context): add per-execution service contract to ExecutionContext"
```
End every commit body in this plan with:
`Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`

---

## Task 2: Re-export moved types from runner as aliases

**Files:**
- Create: `pkg/orchestrator/runner/services_alias.go`
- Modify: `pkg/orchestrator/runner/llm.go`, `tool.go`, `parallel.go`

- [ ] **Step 1: Create `pkg/orchestrator/runner/services_alias.go`**

```go
package runner

import agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"

// These types moved to the context package (so they can live on ExecutionContext
// without an import cycle). Aliases keep existing runner.X references working.
type (
	LLMConfig            = agcontext.LLMConfig
	LLMMessage           = agcontext.LLMMessage
	LLMInvoker           = agcontext.LLMInvoker
	ToolInvoker          = agcontext.ToolInvoker
	GraphExecutor        = agcontext.GraphExecutor
	ContextGraphExecutor = agcontext.ContextGraphExecutor
)
```

- [ ] **Step 2: Delete the original definitions now living in context**

In `llm.go`: delete the `LLMConfig` struct (lines 12–21), the `LLMMessage` struct (23–30), and the `LLMInvoker` interface (32–38). Keep `StreamDeltaFunc`, `LLMRunner`, `SetInvoker`, `Run`, `formatInput`, `init`.

In `tool.go`: delete the `ToolInvoker` interface (22–26). Keep `ToolConfig`, `ToolStartFunc`, `ToolRunner`, `SetInvoker`, `Run`, `init`.

In `parallel.go`: delete the `GraphExecutor` interface (18–22) and `ContextGraphExecutor` interface (24–29). Keep `ParallelConfig`, `ParallelRunner`, `SetExecutor`, `Run`, `dispatchToolCalls`, `init`.

- [ ] **Step 3: Build and test (aliases make all references resolve; adapters implement the same underlying types)**

Run: `go build ./... && go test ./pkg/orchestrator/... ./pkg/agent/... 2>&1 | grep -E "FAIL|ok " | head`
Expected: build succeeds; packages `ok`, no `FAIL`.

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/runner/services_alias.go pkg/orchestrator/runner/llm.go pkg/orchestrator/runner/tool.go pkg/orchestrator/runner/parallel.go
git commit -m "refactor(runner): move service interfaces to context, keep aliases"
```

---

## Task 3: Executor carries and stamps the services

**Files:**
- Modify: `pkg/orchestrator/executor/executor.go`
- Modify: `pkg/agent/agent.go` (set executor services in `NewAIAgent`)

This task is additive: the executor stamps services onto `ec`, but runners still use their own fields, so behavior is unchanged.

- [ ] **Step 1: Add service fields to `Executor`**

In `pkg/orchestrator/executor/executor.go`, extend the struct (it already has `Tracer`) and import agcontext if not already imported (it is, as `agcontext`):

```go
// Executor walks a Graph, executing nodes and routing between them.
type Executor struct {
	Tracer      trace.Tracer
	LLMInvoker  agcontext.LLMInvoker
	ToolInvoker agcontext.ToolInvoker
}
```

- [ ] **Step 2: Stamp services onto the ec at the start of `executeFrom`**

In `executeFrom`, immediately after the function opening (before the `currentNode := startNode` setup loop begins iterating), stamp the context:

```go
func (e *Executor) executeFrom(ctx context.Context, g *orchestrator.Graph,
	startNode string, ec *agcontext.ExecutionContext, startStep int) (interface{}, *orchestrator.ExecutionSnapshot, error) {

	// Inject per-execution services so stateless runners can read them from ec.
	ec.LLMInvoker = e.LLMInvoker
	ec.ToolInvoker = e.ToolInvoker
	ec.Tracer = e.Tracer
	ec.Executor = e

	currentNode := startNode
	// ... unchanged ...
```

(`e` satisfies `agcontext.GraphExecutor`/`ContextGraphExecutor` via its existing `Execute`/`ExecuteWithContext` methods, so `ec.Executor = e` type-checks.)

- [ ] **Step 3: Set executor services in `NewAIAgent`**

In `pkg/agent/agent.go` `NewAIAgent`, after the adapters are built (`a.llmInvoker` / `a.toolInvoker` assigned) and before `a.wireRunners()`, add:

```go
	a.executor.LLMInvoker = a.llmInvoker
	a.executor.ToolInvoker = a.toolInvoker
```

Leave `a.wireRunners()` in place for now (removed in Task 7). `a.executor.Tracer` continues to be set by `SetEventCallback`.

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./pkg/orchestrator/... ./pkg/agent/... 2>&1 | grep -E "FAIL|ok " | head`
Expected: build succeeds; packages `ok` (behavior unchanged — runners still use their own fields).

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/executor/executor.go pkg/agent/agent.go
git commit -m "feat(executor): carry and stamp per-execution services onto ExecutionContext"
```

---

## Task 4: LLMRunner reads services from ec

**Files:**
- Modify: `pkg/orchestrator/runner/llm.go`
- Modify: `pkg/agent/agent.go` (`wireRunners` llm block)

- [ ] **Step 1: Make `LLMRunner` stateless and read from `ec`**

In `llm.go`: remove the `Invoker` and `OnStreamDelta` fields, the `StreamDeltaFunc` type, and the `SetInvoker` method. The struct becomes:

```go
// LLMRunner executes an LLM node by reading its invoker from the ExecutionContext.
type LLMRunner struct{}
```

Rewrite `Run` to resolve the invoker and tracer from `ec`. Replace the `if r.Invoker == nil` block and the streaming block:

```go
func (r *LLMRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg LLMConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*LLMConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	ec, _ := execCtx.(*agcontext.ExecutionContext)
	if ec == nil || ec.LLMInvoker == nil {
		return nil, fmt.Errorf("llm runner: no invoker configured")
	}

	// Build messages: prefer full conversation history from ConvMem
	var messages []LLMMessage
	if ec.ConvMem != nil && len(ec.ConvMem.Messages) > 0 {
		for _, m := range ec.ConvMem.Messages {
			messages = append(messages, LLMMessage{
				Role:       m.Role,
				Content:    m.Content,
				Name:       m.Name,
				ToolCalls:  m.ToolCalls,
				ToolCallID: m.ToolCallID,
			})
		}
	} else {
		if cfg.SystemPrompt != "" {
			messages = append(messages, LLMMessage{Role: "system", Content: cfg.SystemPrompt})
		}
		messages = append(messages, LLMMessage{Role: "user", Content: formatInput(input)})
	}

	// Stream when a tracer is present so deltas reach the display.
	var result *orchestrator.NodeResult
	var callErr error
	if ec.Tracer != nil {
		result, callErr = ec.LLMInvoker.ChatStream(ctx, cfg.Model, messages, cfg.Tools, cfg, func(delta string) {
			ec.Tracer.OnStreamDelta(ctx, delta)
		})
	} else {
		result, callErr = ec.LLMInvoker.Chat(ctx, cfg.Model, messages, cfg.Tools, cfg)
	}
	if callErr != nil {
		return nil, callErr
	}

	// Append assistant message to ConvMem (unchanged logic).
	if ec.ConvMem != nil && result != nil {
		if outMap, ok := result.Output.(map[string]interface{}); ok {
			asstMsg := agcontext.Message{Role: "assistant"}
			if c, ok := outMap["content"].(string); ok {
				asstMsg.Content = c
			}
			if tcs, ok := outMap["tool_calls"].([]map[string]interface{}); ok {
				asstMsg.ToolCalls = tcs
			}
			ec.ConvMem.AddMessage(asstMsg)
		}
	}

	return result, nil
}
```

(Behavior note: previously streaming was used only when an `OnStreamDelta` was wired; now it's used whenever a tracer is present. The NopTracer's `OnStreamDelta` is a no-op, so this is equivalent for non-streaming agents.)

- [ ] **Step 2: Remove the llm block from `wireRunners`**

In `pkg/agent/agent.go` `wireRunners`, delete the entire `if entry, ok := orchestrator.LookupNodeType("llm"); ok { ... }` block (it set `SetInvoker` and `OnStreamDelta`). Leave the `tool` and `parallel` blocks.

- [ ] **Step 3: Build and run the agent + runner tests**

Run: `go build ./... && go test ./pkg/agent/ ./pkg/orchestrator/runner/ 2>&1 | grep -E "FAIL|ok " | head`
Expected: build succeeds; `ok`. (LLM now resolves its invoker from the executor-stamped ec; the agent set `a.executor.LLMInvoker` in Task 3.)

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/runner/llm.go pkg/agent/agent.go
git commit -m "refactor(runner): LLMRunner reads invoker/tracer from ExecutionContext"
```

---

## Task 5: ToolRunner reads services from ec

**Files:**
- Modify: `pkg/orchestrator/runner/tool.go`
- Modify: `pkg/agent/agent.go` (`wireRunners` tool block)
- Modify: `pkg/orchestrator/runner/runner_test.go` (mock invoker via ec)

- [ ] **Step 1: Write/adjust the failing test first**

In `runner_test.go`, rewrite `TestToolRunnerWithMockInvoker` to pass the invoker through an `ExecutionContext` instead of `SetInvoker`. Add the agcontext import.

```go
func TestToolRunnerWithMockInvoker(t *testing.T) {
	r := &ToolRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Resource":"rpc/test","Timeout":10}`),
	}
	ec := agcontext.NewExecutionContext(nil)
	ec.ToolInvoker = &mockToolInvoker{}
	result, err := r.Run(context.Background(), node, "input", ec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != orchestrator.StatusContinue {
		t.Fatalf("expected status 'continue', got %q", result.Status)
	}
	if result.Output != "result" {
		t.Fatalf("expected output 'result', got %v", result.Output)
	}
}
```

Add to the imports of `runner_test.go`:
```go
agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
```

- [ ] **Step 2: Run it — expect FAIL (ToolRunner still uses r.Invoker, which is nil)**

Run: `go test ./pkg/orchestrator/runner/ -run TestToolRunnerWithMockInvoker -v`
Expected: FAIL with "tool runner: no invoker configured" (the ec-supplied invoker isn't read yet).

- [ ] **Step 3: Make `ToolRunner` stateless and read from `ec`**

In `tool.go`: remove the `Invoker` and `OnToolStart` fields, the `ToolStartFunc` type, and the `SetInvoker` method. The struct becomes:

```go
// ToolRunner executes a tool by reading its invoker from the ExecutionContext.
type ToolRunner struct{}
```

In `Run`, replace the invoker check and the OnToolStart call. The relevant edits:

Replace
```go
	if r.Invoker == nil {
		return nil, fmt.Errorf("tool runner: no invoker configured")
	}
```
with
```go
	ec, _ := execCtx.(*agcontext.ExecutionContext)
	if ec == nil || ec.ToolInvoker == nil {
		return nil, fmt.Errorf("tool runner: no invoker configured")
	}
```

Replace the start-event block
```go
	// Fire start event BEFORE invoke so long tools give live feedback.
	if r.OnToolStart != nil {
		r.OnToolStart(ctx, resource, toolArgsStr)
	}

	result, err := r.Invoker.Invoke(ctx, resource, input, cfg.Timeout)
```
with
```go
	// Fire start event BEFORE invoke so long tools give live feedback.
	if ec.Tracer != nil {
		ec.Tracer.OnToolStart(ctx, resource, toolArgsStr)
	}

	result, err := ec.ToolInvoker.Invoke(ctx, resource, input, cfg.Timeout)
```

The later `execCtx.(*agcontext.ExecutionContext)` block that appends the tool result to ConvMem can now reuse the `ec` resolved at the top — change `if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.ConvMem != nil {` to `if ec.ConvMem != nil {`.

- [ ] **Step 4: Remove the tool block from `wireRunners`**

In `pkg/agent/agent.go` `wireRunners`, delete the entire `if entry, ok := orchestrator.LookupNodeType("tool"); ok { ... }` block (it set `SetInvoker` and `OnToolStart`). Leave only the `parallel` block.

- [ ] **Step 5: Run runner + agent tests**

Run: `go build ./... && go test ./pkg/orchestrator/runner/ ./pkg/agent/ 2>&1 | grep -E "FAIL|ok " | head`
Expected: build succeeds; `TestToolRunnerWithMockInvoker` and `TestToolRunnerNoInvoker` pass; packages `ok`.

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/runner/tool.go pkg/orchestrator/runner/runner_test.go pkg/agent/agent.go
git commit -m "refactor(runner): ToolRunner reads invoker/tracer from ExecutionContext"
```

---

## Task 6: ParallelRunner reads sub-executor from ec

**Files:**
- Modify: `pkg/orchestrator/runner/parallel.go`
- Modify: `pkg/agent/agent.go` (`wireRunners` parallel block)
- Modify: `pkg/orchestrator/runner/runner_test.go` (executor via ec)

- [ ] **Step 1: Adjust the failing test first**

In `runner_test.go`, rewrite `TestParallelRunnerNoExecutor` so it passes an ec without an executor and still expects the error. (After the refactor, `r.Run(..., nil)` would also error, but be explicit with an empty ec.)

```go
func TestParallelRunnerNoExecutor(t *testing.T) {
	r := &ParallelRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Branches":[]}`),
	}
	ec := agcontext.NewExecutionContext(nil) // no Executor set
	_, err := r.Run(context.Background(), node, nil, ec)
	if err == nil {
		t.Fatal("expected error for missing executor")
	}
}
```

- [ ] **Step 2: Run it — expect FAIL (ParallelRunner still checks r.Executor, which is nil → already errors, so this may already pass for the wrong reason)**

Run: `go test ./pkg/orchestrator/runner/ -run TestParallelRunnerNoExecutor -v`
Expected: PASS currently (r.Executor nil → error). This test guards the post-refactor behavior; proceed.

- [ ] **Step 3: Make `ParallelRunner` stateless and read `ec.Executor`**

In `parallel.go`: remove the `Executor` field and `SetExecutor` method. The struct becomes:

```go
// ParallelRunner executes multiple branches concurrently, using the sub-executor
// from the ExecutionContext.
type ParallelRunner struct{}
```

In `Run`, resolve the executor and ec at the top, replacing the `if r.Executor == nil` check:

```go
	ec, _ := execCtx.(*agcontext.ExecutionContext)
	if ec == nil || ec.Executor == nil {
		return nil, fmt.Errorf("parallel runner: no executor configured")
	}
	exec := ec.Executor
```

Then replace every `r.Executor` usage with `exec`:
- static branch loop: `out, _, err := exec.Execute(ctx, g, input)`
- in `dispatchToolCalls`, change its signature to also receive the executor, or re-resolve from `execCtx`. Simplest: re-resolve inside `dispatchToolCalls` (it already type-asserts `execCtx` for `parentEC`). Replace:
  ```go
  ctxExec, hasCtxExec := r.Executor.(ContextGraphExecutor)
  ```
  with (using the parentEC already resolved a few lines below — move that resolution up, or resolve exec here):
  ```go
  var exec GraphExecutor
  if ec, ok := execCtx.(*agcontext.ExecutionContext); ok {
      exec = ec.Executor
  }
  ctxExec, hasCtxExec := exec.(ContextGraphExecutor)
  ```
  and replace `r.Executor.Execute(ctx, subGraph, toolInput)` with `exec.Execute(ctx, subGraph, toolInput)`.

- [ ] **Step 4: Remove the parallel block from `wireRunners`**

In `pkg/agent/agent.go` `wireRunners`, delete the `if entry, ok := orchestrator.LookupNodeType("parallel"); ok { ... }` block. `wireRunners` is now an empty body (deleted entirely in Task 7).

- [ ] **Step 5: Build and test**

Run: `go build ./... && go test ./pkg/orchestrator/runner/ ./pkg/agent/ 2>&1 | grep -E "FAIL|ok " | head`
Expected: build succeeds; `ok`.

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/runner/parallel.go pkg/orchestrator/runner/runner_test.go pkg/agent/agent.go
git commit -m "refactor(runner): ParallelRunner reads sub-executor from ExecutionContext"
```

---

## Task 7: Delete the now-empty wireRunners

**Files:**
- Modify: `pkg/agent/agent.go`

- [ ] **Step 1: Remove `wireRunners` and its call**

In `pkg/agent/agent.go`: delete the now-empty `func (a *AIAgent) wireRunners() { ... }` (including the `TODO(tech-debt)` comment above it — the debt is resolved) and delete the `a.wireRunners()` call in `NewAIAgent`.

Confirm `NewAIAgent` still sets `a.executor.LLMInvoker = a.llmInvoker` and `a.executor.ToolInvoker = a.toolInvoker` (added in Task 3).

- [ ] **Step 2: Build and full test**

Run: `go build ./... && go test ./... 2>&1 | grep -E "FAIL|panic" | head; echo done`
Expected: build succeeds; no `FAIL`/`panic`.

- [ ] **Step 3: Commit**

```bash
git add pkg/agent/agent.go
git commit -m "refactor(agent): delete wireRunners — runners are now stateless"
```

---

## Task 8: Flip regression tests + add race test + final verification

**Files:**
- Modify: `pkg/agent/delegate_invoker_leak_test.go`
- Create: `pkg/agent/delegate_race_test.go`

- [ ] **Step 1: Flip the invoker-leak test to assert the fix**

Replace the body of `TestDelegationLeaksChildInvokerGlobally` in `pkg/agent/delegate_invoker_leak_test.go` with a positive assertion, and rename it. The helper `globalLLMInvokerDisablesDelegate` is no longer meaningful (there is no global invoker), so this test now verifies that a child agent's invoker disables delegate while the parent's does not — i.e., each agent uses its OWN invoker.

```go
// TestDelegationKeepsParentInvoker verifies that creating a child agent (which
// disables delegate tools for itself) does NOT affect the parent's own invoker.
// Previously a shared global runner leaked the child's restricted invoker to the
// parent, so async delegation only worked on the first request.
func TestDelegationKeepsParentInvoker(t *testing.T) {
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if adapterDisablesDelegate(parent.llmInvoker) {
		t.Fatal("parent invoker should offer delegate tools")
	}

	child, err := parent.NewChildAgent("sub task")
	if err != nil {
		t.Fatal(err)
	}
	if !adapterDisablesDelegate(child.llmInvoker) {
		t.Fatal("child invoker should disable delegate tools")
	}
	// The parent's invoker must be untouched by the delegation.
	if adapterDisablesDelegate(parent.llmInvoker) {
		t.Fatal("parent invoker was mutated by delegation (state leak)")
	}
}

func adapterDisablesDelegate(inv interface{}) bool {
	ra, ok := inv.(*adapters.RouterAdapter)
	if !ok {
		return false
	}
	for _, d := range ra.Config.DisabledTools {
		if d == "delegate_task_async" {
			return true
		}
	}
	return false
}
```

Delete the old `globalLLMInvokerDisablesDelegate` helper and the old test function. Keep the `adapters`/`types` imports; drop the `orchestrator`/`orchrunner` imports if now unused.

- [ ] **Step 2: Run it — expect PASS (each agent keeps its own invoker)**

Run: `go test ./pkg/agent/ -run TestDelegationKeepsParentInvoker -v`
Expected: PASS.

- [ ] **Step 3: Add a `-race` concurrent-delegation test**

Create `pkg/agent/delegate_race_test.go`:

```go
package agent

import (
	"sync"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// TestConcurrentChildAgentsNoRace creates many child agents concurrently. With
// the old global-singleton runners this raced on shared mutable fields; with
// per-execution services there is no shared mutable state. Run with -race.
func TestConcurrentChildAgentsNoRace(t *testing.T) {
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent.SetEventCallback(func(Event) {})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			child, err := parent.NewChildAgent("task")
			if err != nil {
				t.Error(err)
				return
			}
			// Touch the child's executor services the way a real run would read them.
			_ = child.executor
		}()
	}
	wg.Wait()
}
```

- [ ] **Step 4: Run the race test**

Run: `go test ./pkg/agent/ -run TestConcurrentChildAgentsNoRace -race -v`
Expected: PASS, no `DATA RACE` report.

- [ ] **Step 5: Full verification**

Run each and confirm:
- `gofmt -l pkg/orchestrator/context pkg/orchestrator/runner pkg/orchestrator/executor pkg/agent` → no output
- `go vet ./...` → no errors
- `go build ./...` → succeeds
- `go test ./...` → all `ok`, no `FAIL`
- `go test ./pkg/agent/ ./pkg/orchestrator/... -race` → no `DATA RACE`

- [ ] **Step 6: Commit**

```bash
git add pkg/agent/delegate_invoker_leak_test.go pkg/agent/delegate_race_test.go
git commit -m "test(agent): verify per-agent invokers survive delegation + race-free concurrency"
```

---

## Self-Review Notes

- **Spec coverage:** service contract in context + LLMMessage/LLMConfig moved (Task 1–2) ✓; ExecutionContext fields (Task 1) ✓; runner aliases (Task 2) ✓; stateless runners reading ec (Tasks 4–6) ✓; OnStreamDelta/OnToolStart func-fields removed, call ec.Tracer directly (Tasks 4–5) ✓; Executor carries+stamps (Task 3) ✓; wireRunners deleted, agent sets executor services (Tasks 3,7) ✓; child event forwarding unchanged (untouched — still in NewChildAgent) ✓; tests flipped + race test (Task 8) ✓.
- **Success criteria:** parent keeps delegate tools after delegation (Task 8 `TestDelegationKeepsParentInvoker`); `-race` clean (Task 8 `TestConcurrentChildAgentsNoRace`); child uses own restricted invoker (Task 8); `[子Agent]` labeling + silencing preserved (NewChildAgent forwarding untouched, now flows through per-ec Tracer); full suite green (Task 7–8).
- **Type consistency:** `agcontext.{LLMInvoker,ToolInvoker,GraphExecutor,ContextGraphExecutor,LLMMessage,LLMConfig}` defined Task 1, aliased in runner Task 2; `Executor.{LLMInvoker,ToolInvoker,Tracer}` + `ec.{LLMInvoker,ToolInvoker,Executor,Tracer}` used consistently Tasks 3–6; `adapters.RouterAdapter.Config.DisabledTools` used in Task 8 helper.
- **Known limitation:** `NewChildAgent`'s event-forwarding wiring (drop stream, mark `FromSubAgent`) is intentionally unchanged; it now routes through the child's per-ec Tracer rather than the global runner, which is strictly more correct.
