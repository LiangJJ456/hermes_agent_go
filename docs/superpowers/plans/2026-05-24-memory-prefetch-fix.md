# Memory Prefetch Dedup/Relevance Fix — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复每轮对话中记忆预取被调用两次(有意义的那次丢结果、注入的那次传空 query)的 bug,并给 mempalace 召回加相关性闸门、让 builtin 退出每轮预取。

**Architecture:** 预取改为每轮在 `Run` 中用真实 `userInput` 调一次,结果包成 `<memory-context>` 块缓存到新字段 `AIAgent.pendingMemoryCtx`,`conversationLoop` 每个迭代从该缓存做非持久化注入,轮末清空。mempalace `Prefetch` 在格式化前用纯 query-命中信号(`VectorScore`/`BM25Score`)过滤掉只靠 importance 撑分的不相关抽屉。builtin `Prefetch` 改为返回空、退出预取。

**Tech Stack:** Go 1.25,标准库 `testing`(repo 已用 testify 但本计划的测试用标准库即可)。

**Spec:** `docs/superpowers/specs/2026-05-24-memory-prefetch-fix-design.md`

**Git identity note:** 本仓库未配置 `user.name`/`user.email`。若提交报 "Author identity unknown",用一次性身份提交,例如:
`git -c user.email="zerolook70@gmail.com" -c user.name="zerolook70" commit -m "..."`(不写入 config)。

---

## File Structure

- **Modify** `pkg/agent/memory/mempalace/provider.go` — 加 prefetch 阈值常量 + `filterRelevant` helper;`Prefetch` 扩大候选池并接入过滤。
- **Create** `pkg/agent/memory/mempalace/prefetch_filter_test.go` — `filterRelevant` 纯函数单测。
- **Modify** `pkg/agent/memory/builtin.go` — `Prefetch` 返回空、`QueuePrefetch` no-op、删 `prefetchCache`/`prefetchReady`/`mu` 字段与 `buildPrefetchContent`、删 `sync` import。
- **Create** `pkg/agent/memory/builtin_test.go` — 验证 builtin `Prefetch` 返回空。
- **Modify** `pkg/agent/agent.go` — `AIAgent` 加 `pendingMemoryCtx` 字段;`Run` 每轮预取一次并缓存、轮末清空;`conversationLoop` 改为读缓存注入。
- **Create** `pkg/agent/agent_test.go` — fake model + fake memory provider,验证每轮 `Prefetch` 用真 query 调用恰一次且轮末清空。

---

## Task 1: mempalace 相关性闸门

**Files:**
- Test: `pkg/agent/memory/mempalace/prefetch_filter_test.go` (create)
- Modify: `pkg/agent/memory/mempalace/provider.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/agent/memory/mempalace/prefetch_filter_test.go`:

```go
package mempalace

import "testing"

func TestFilterRelevantKeepsQueryHits(t *testing.T) {
	results := []SearchResult{
		{Score: 0.9, VectorScore: 0.5, BM25Score: 0},   // 向量命中 → 保留
		{Score: 0.8, VectorScore: 0, BM25Score: 1.2},   // 关键词命中 → 保留
		{Score: 0.7, VectorScore: 0.1, BM25Score: 0},   // 仅 importance、query 零命中 → 剔除
	}
	kept := filterRelevant(results, 0.35, 0.0, 3)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d", len(kept))
	}
}

func TestFilterRelevantDropsImportanceOnly(t *testing.T) {
	results := []SearchResult{
		{VectorScore: 0.1, BM25Score: 0},
		{VectorScore: 0.2, BM25Score: 0},
	}
	if got := filterRelevant(results, 0.35, 0.0, 3); len(got) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(got))
	}
}

func TestFilterRelevantCapsAtLimit(t *testing.T) {
	var results []SearchResult
	for i := 0; i < 6; i++ {
		results = append(results, SearchResult{VectorScore: 0.9, BM25Score: 1})
	}
	if got := filterRelevant(results, 0.35, 0.0, 3); len(got) != 3 {
		t.Fatalf("expected cap 3, got %d", len(got))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/agent/memory/mempalace/ -run TestFilterRelevant -v`
Expected: 编译失败 `undefined: filterRelevant`。

- [ ] **Step 3: Add constants + filterRelevant in provider.go**

在 `pkg/agent/memory/mempalace/provider.go` 中,`Prefetch` 函数(当前第 160 行附近)**之前**插入:

```go
// 预取相关性闸门参数
const (
	prefetchCandidatePool = 8    // L3 候选池大小(过滤前)
	prefetchTopN          = 3    // 过滤后注入上限
	prefetchMinVecSim     = 0.35 // 向量相似度下限
	prefetchBM25Epsilon   = 0.0  // BM25 命中阈值(>epsilon 视为真实 query 重叠)
)

// filterRelevant 仅保留有真实 query 命中信号的结果,剔除只靠 importance 撑分、
// query 零命中的抽屉,然后截断到 limit 条。
func filterRelevant(results []SearchResult, minVecSim, bm25Epsilon float64, limit int) []SearchResult {
	kept := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.VectorScore >= minVecSim || r.BM25Score > bm25Epsilon {
			kept = append(kept, r)
		}
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/agent/memory/mempalace/ -run TestFilterRelevant -v`
Expected: 3 个测试 PASS。

- [ ] **Step 5: Wire filterRelevant into Prefetch**

在 `provider.go` 中将 `Prefetch` 的开头(当前):

```go
func (p *Provider) Prefetch(_ context.Context, query string, _ string) string {
	if p.stack == nil || query == "" {
		return ""
	}

	// L3 search for relevant memories
	results := p.stack.L3.SearchRaw(query, "", "", 3)
	if len(results) == 0 {
		return ""
	}
```

替换为:

```go
func (p *Provider) Prefetch(_ context.Context, query string, _ string) string {
	if p.stack == nil || query == "" {
		return ""
	}

	// L3 search,然后按真实 query 相关性过滤(剔除仅 importance 撑分的抽屉)
	results := p.stack.L3.SearchRaw(query, "", "", prefetchCandidatePool)
	results = filterRelevant(results, prefetchMinVecSim, prefetchBM25Epsilon, prefetchTopN)
	if len(results) == 0 {
		return ""
	}
```

(`Prefetch` 其余部分——遍历 results 构建 `parts`、KG 查询、`return strings.Join(...)`——保持不变。)

- [ ] **Step 6: Verify package builds and all mempalace tests pass**

Run: `go build ./... && go test ./pkg/agent/memory/mempalace/ -v`
Expected: 构建成功;`TestFilterRelevant*` PASS。

- [ ] **Step 7: Commit**

```bash
git add pkg/agent/memory/mempalace/provider.go pkg/agent/memory/mempalace/prefetch_filter_test.go
git commit -m "feat(mempalace): gate prefetch recall by query relevance"
```
(若报 identity 错误,见计划头部的 git identity note。)

---

## Task 2: builtin 退出每轮预取

**Files:**
- Test: `pkg/agent/memory/builtin_test.go` (create)
- Modify: `pkg/agent/memory/builtin.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/agent/memory/builtin_test.go`:

```go
package memory

import (
	"context"
	"testing"
)

func TestBuiltinProviderPrefetchReturnsEmpty(t *testing.T) {
	store := NewStore(t.TempDir(), 0, 0)
	p := NewBuiltinProvider(store)

	// 即使存有内容,builtin 也不应通过预取注入(它只走 SystemPromptBlock)
	p.store.Add("memory", "some durable note")

	if got := p.Prefetch(context.Background(), "note", "sess"); got != "" {
		t.Fatalf("expected empty prefetch, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/agent/memory/ -run TestBuiltinProviderPrefetch -v`
Expected: FAIL — 当前 `Prefetch` 会返回 "some durable note"(非空)。

- [ ] **Step 3: Strip builtin prefetch — struct fields**

在 `pkg/agent/memory/builtin.go` 中,将结构体(当前):

```go
type BuiltinProvider struct {
	store *Store

	mu            sync.Mutex
	prefetchCache string // 缓存的预取内容
	prefetchReady bool   // 是否有待消费的预取结果
}
```

替换为:

```go
type BuiltinProvider struct {
	store *Store
}
```

- [ ] **Step 4: Strip builtin prefetch — remove sync import**

将 import 块(当前):

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)
```

替换为(去掉 `"sync"`):

```go
import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)
```

- [ ] **Step 5: Strip builtin prefetch — Prefetch + QueuePrefetch bodies**

将 `Prefetch`(当前注释 `// Prefetch 返回缓存的预取内容...` 到函数结束)和 `QueuePrefetch`(当前注释 `// QueuePrefetch 异步预取...` 到函数结束)这两个函数整体替换为:

```go
// Prefetch — builtin 记忆不参与每轮预取,仅经 SystemPromptBlock 注入 system prompt。
func (p *BuiltinProvider) Prefetch(_ context.Context, _ string, _ string) string {
	return ""
}

// QueuePrefetch — builtin 已退出预取,no-op。
func (p *BuiltinProvider) QueuePrefetch(_ string, _ string) {}
```

- [ ] **Step 6: Strip builtin prefetch — remove buildPrefetchContent**

删除整个 `buildPrefetchContent` 方法(当前注释 `// buildPrefetchContent 从当前 store 状态构建预取内容` 到其函数结束的 `}`)。保留其后的 `errJSON` 函数不动。

- [ ] **Step 7: Run test + build to verify pass**

Run: `go build ./... && go test ./pkg/agent/memory/ -run TestBuiltinProviderPrefetch -v`
Expected: 构建成功(无 unused import/field 错误);测试 PASS。

- [ ] **Step 8: Commit**

```bash
git add pkg/agent/memory/builtin.go pkg/agent/memory/builtin_test.go
git commit -m "feat(memory): drop builtin provider from per-turn prefetch"
```

---

## Task 3: agent 每轮预取一次 + 缓存注入

**Files:**
- Test: `pkg/agent/agent_test.go` (create)
- Modify: `pkg/agent/agent.go`

- [ ] **Step 1: Add the pendingMemoryCtx field**

在 `pkg/agent/agent.go` 的 `AIAgent` 结构体中,将(当前第 65-66 行附近):

```go
	// 记忆系统
	memoryMgr *memory.Manager
```

替换为:

```go
	// 记忆系统
	memoryMgr *memory.Manager

	// 本轮记忆预取缓存:每轮在 Run 中用真实 query 构建,conversationLoop 注入,轮末清空
	pendingMemoryCtx string
```

- [ ] **Step 2: Write the failing test**

Create `pkg/agent/agent_test.go`:

```go
package agent

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// fakeModelProvider 返回一个无 tool_call 的终态响应,使 conversationLoop 一轮即结束。
type fakeModelProvider struct{}

func (fakeModelProvider) Name() string { return "fake" }
func (fakeModelProvider) Chat(_ context.Context, _ *model.ChatRequest) (*types.ChatResponse, error) {
	return &types.ChatResponse{Message: types.Message{Role: types.RoleAssistant, Content: "done"}}, nil
}
func (fakeModelProvider) ChatStream(_ context.Context, _ *model.ChatRequest, _ model.StreamCallback) (*types.ChatResponse, error) {
	return &types.ChatResponse{Message: types.Message{Role: types.RoleAssistant, Content: "done"}}, nil
}
func (fakeModelProvider) SupportsTools() bool   { return true }
func (fakeModelProvider) MaxContextTokens() int { return 128000 }

// fakeMemProvider 记录 Prefetch 调用次数与传入 query。
type fakeMemProvider struct {
	memory.BaseProvider
	prefetchCalls   int
	prefetchQueries []string
}

func (f *fakeMemProvider) Name() string                                       { return "fake-mem" }
func (f *fakeMemProvider) IsAvailable() bool                                  { return true }
func (f *fakeMemProvider) Initialize(_ context.Context, _ memory.InitOpts) error { return nil }
func (f *fakeMemProvider) Prefetch(_ context.Context, query string, _ string) string {
	f.prefetchCalls++
	f.prefetchQueries = append(f.prefetchQueries, query)
	return "recalled: " + query
}

func TestRunPrefetchesOncePerTurnWithUserInput(t *testing.T) {
	router := model.NewRouter()
	router.Register(fakeModelProvider{})

	cfg := types.AgentConfig{
		Model:            "fake/m",
		MaxIterations:    90,
		MaxParallelTools: 1,
		Platform:         "cli",
	}
	ag := NewAIAgent(cfg, router, registry.Global())

	mem := &fakeMemProvider{}
	mgr := memory.NewManager()
	if err := mgr.AddProvider(mem); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	ag.SetMemoryManager(mgr)

	if _, err := ag.Run(context.Background(), "hello world"); err != nil {
		t.Fatalf("run: %v", err)
	}

	if mem.prefetchCalls != 1 {
		t.Fatalf("expected Prefetch called once, got %d", mem.prefetchCalls)
	}
	if len(mem.prefetchQueries) != 1 || mem.prefetchQueries[0] != "hello world" {
		t.Fatalf("expected query [hello world], got %v", mem.prefetchQueries)
	}
	if ag.pendingMemoryCtx != "" {
		t.Fatalf("expected pendingMemoryCtx cleared after turn, got %q", ag.pendingMemoryCtx)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/agent/ -run TestRunPrefetchesOncePerTurn -v`
Expected: FAIL — 当前 `Run` 与 `conversationLoop` 共调用 `Prefetch` 两次(一次真 query、一次空 query),`prefetchCalls` != 1。

- [ ] **Step 4: Rewrite the prefetch block in Run**

在 `agent.go` 的 `Run` 中,将(当前第 302-322 行):

```go
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
```

替换为:

```go
	// pre-turn: 记忆预取(每轮一次,用真实 query;结果包成上下文块缓存,供本轮注入)
	if a.memoryMgr != nil {
		a.memoryMgr.OnTurnStart(a.turnNum, userInput, nil)
		memCtx := a.memoryMgr.PrefetchAll(ctx, userInput, a.config.SessionID)
		block := memory.BuildContextBlock(memCtx)
		a.mu.Lock()
		a.pendingMemoryCtx = block
		a.mu.Unlock()
		if block != "" {
			a.emitEvent(Event{Type: EventMemory, Content: "memory context recalled"})
		}
	}

	// 核心对话循环
	reply, err := a.conversationLoop(ctx)

	// 清空本轮预取缓存
	a.mu.Lock()
	a.pendingMemoryCtx = ""
	a.mu.Unlock()

	// post-turn: 同步记忆
	if err == nil && a.memoryMgr != nil {
		a.memoryMgr.SyncAll(userInput, reply, a.config.SessionID)
		a.memoryMgr.QueuePrefetchAll(userInput, a.config.SessionID)
	}

	return reply, err
}
```

- [ ] **Step 5: Rewrite the injection block in conversationLoop**

在 `agent.go` 的 `conversationLoop` 中,将(当前第 372-399 行):

```go
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
```

替换为:

```go
		// 注入本轮预取的记忆上下文(已在 Run 中构建并缓存;非持久化注入,保持持久化前缀稳定)
		a.mu.Lock()
		contextBlock := a.pendingMemoryCtx
		a.mu.Unlock()
		if contextBlock != "" {
			// 在最后一条用户消息之前插入记忆上下文
			lastUserIdx := -1
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == types.RoleUser {
					lastUserIdx = i
					break
				}
			}
			if lastUserIdx > 0 {
				injected := make([]types.Message, 0, len(msgs)+1)
				injected = append(injected, msgs[:lastUserIdx]...)
				injected = append(injected, types.Message{
					Role:    types.RoleSystem,
					Content: contextBlock,
				})
				injected = append(injected, msgs[lastUserIdx:]...)
				msgs = injected
			}
		}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./pkg/agent/ -run TestRunPrefetchesOncePerTurn -v`
Expected: PASS。

- [ ] **Step 7: Full build + full test suite**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 构建/vet 干净;全部包测试通过(无回归)。

- [ ] **Step 8: Commit**

```bash
git add pkg/agent/agent.go pkg/agent/agent_test.go
git commit -m "fix(agent): prefetch memory once per turn with real query"
```

---

## Verification (after all tasks)

- [ ] `go build ./...` 成功
- [ ] `go vet ./...` 无输出
- [ ] `go test ./...` 全绿
- [ ] 人工确认:`pkg/agent/memory/builtin.go` 无残留 `prefetchCache`/`prefetchReady`/`buildPrefetchContent`/`sync` 引用(`grep -n "prefetch\|sync" pkg/agent/memory/builtin.go` 仅剩注释/QueuePrefetch 签名)
- [ ] 人工确认:`pkg/agent/agent.go` 中 `PrefetchAll` 仅在 `Run` 出现一次,`conversationLoop` 不再调用它
