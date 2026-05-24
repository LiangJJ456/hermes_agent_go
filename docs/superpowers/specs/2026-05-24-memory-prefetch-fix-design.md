# 记忆预取重复/失效修复 — 设计文档

日期:2026-05-24
范围:`pkg/agent/agent.go`、`pkg/agent/memory/builtin.go`、`pkg/agent/memory/mempalace/provider.go`

## 背景与问题

每轮对话中,记忆预取被调用了两次,且语义不一致:

1. **`Run` 内(`agent.go:305`)** 用真实 `userInput` 调 `PrefetchAll`,但结果被 `_ = memCtx` 丢弃,只发了一个误导性的 `EventMemory("memory context recalled")` 事件——实际没有任何内容注入。
2. **`conversationLoop` 内(`agent.go:374`,位于每迭代的 `for` 循环中)** 用**空 query** 调 `PrefetchAll(ctx, "", ...)`,其结果才是真正注入上下文的那个。

### 根因

两个预取点 query 不一致:有意义的那次(真 query)丢结果,注入的那次传空串。

### 净后果

| 组件 | 现状 |
|---|---|
| mempalace L3 按需召回 | **完全失效**。`Prefetch` 在 `query==""` 时直接 `return ""`(`provider.go:162`);唯一带真 query 的调用又把结果丢了。语义记忆只能靠模型显式调 `palace_search` 才拿得到。 |
| builtin 记忆 | 仍能注入(每迭代从磁盘重建),但 `QueuePrefetch` 预热的缓存被丢弃调用浪费,且每个 LLM 往返都重读一次磁盘。 |
| `EventMemory` 事件 | 误导——声称"已召回",实际没注入。 |

### 附带发现:相关性问题

mempalace 检索打分 `finalScore = 0.6*vecSim + 0.4*bm25Norm + entityBoost + importance/10`。其中:
- **importance boost 无条件叠加**——一条 query 零命中、但 importance 高的抽屉照样拿高分。
- BM25 经 min-max 归一化,top 结果恒为 1.0。
- fallback(纯 BM25)路径的 `finalScore > 0.01` 闸门形同虚设:importance=3 即贡献 0.3,任何有 importance 的抽屉都能过。

结果:`Prefetch` 取 top-3 时,可能召回的是"按 importance 排"而非"按相关性排"的抽屉,即"什么都召回"。

## 设计

### 改动 1:每轮只预取一次(修重复 + 空 query)

- `AIAgent` 新增字段 `pendingMemoryCtx string`,由现有 `mu` 保护。
- `Run` 中追加完 user 消息后:用**真实 `userInput`** 调一次 `PrefetchAll` → 经 `memory.BuildContextBlock` 包装 → 存入 `a.pendingMemoryCtx`。**仅当非空**才发 `EventMemory`。
- `conversationLoop` 中删除 `PrefetchAll(ctx, "", ...)` 调用,改为读取 `a.pendingMemoryCtx`:非空则按原有插入逻辑放到最后一条 user 消息之前(**仍是非持久化注入**,保持持久化历史前缀稳定、cache 友好),不再重新检索。
- `Run` 在 `conversationLoop` 返回后清空 `a.pendingMemoryCtx`。

效果:每轮检索 1 次、使用真实 query、mempalace L3 召回真正生效。

### 改动 2:mempalace 召回加相关性闸门

仅作用于 `Provider.Prefetch`(**不改** `palace_search` 工具——那是用户显式查询,应保持宽召回)。

- 候选池从 top-3 扩大到 top-8:`SearchRaw(query, "", "", 8)`,以免高 importance 的不相关抽屉挤掉真正相关项后凑不满。
- 相关性闸门(键在真实 query 命中信号,而非被 importance 污染的 `finalScore`):

  > 保留条件:`VectorScore >= minVecSim` **或** `BM25Score > bm25Epsilon`

  - `minVecSim` = 0.35(cosine 相似度下限)
  - `bm25Epsilon` = 0.0(`BM25Score` 在 hybrid 路径是纯 BM25 原始分,fallback 路径含 wing/room 命中加成,两者 `>0` 都意味着真实 query 重叠)
- 过滤后取 top-3。全部被过滤掉则注入空(宁缺毋滥)。
- 阈值定义为包级常量,便于后续提到 config。

### 改动 3:builtin 退出每轮预取

- `BuiltinProvider.Prefetch` 改为始终返回 `""`;`QueuePrefetch` 改为 no-op。
- 删除随之失效的死代码:`prefetchCache`、`prefetchReady`、`mu`(若仅服务于预取缓存)、`buildPrefetchContent`。
- builtin 记忆(用户画像 + 长期笔记)属身份级常驻内容,继续仅经 `SystemPromptBlock` 注入 system prompt。

#### 已知后果(用户已确认接受)

system prompt 仅在首轮(`len(a.messages)==0`)构建一次,因此**会话中途经 `memory` 工具写入的内容,本会话内不会自动出现在上下文**;模型需主动调用 `memory`(action=read)查看。若日后需要 builtin 的会话内新鲜度,应另开改动(每轮刷新 system prompt 的记忆块),不在本次范围。

## 测试

当前 `pkg/agent` 与 `pkg/agent/memory/mempalace` 均无测试。本次新增:

- **mempalace 相关性闸门**(`provider_test.go` 或 searcher 层):构造若干抽屉,验证
  - query 命中的抽屉被召回;
  - 仅靠高 importance、query 零命中的抽屉被剔除;
  - 全不相关时返回空。
- **agent 预取一次性语义**:用一个 fake/stub `memory.Provider` 记录 `Prefetch` 被调用的次数与传入 query,验证
  - 每轮 `Prefetch` 以真实 `userInput` 被调用恰好一次(不再有空 query 调用);
  - `pendingMemoryCtx` 在轮末被清空;
  - 注入块出现在最后一条 user 消息之前。
- **builtin 退出预取**:验证 `BuiltinProvider.Prefetch` 返回 `""`。

## 不在范围内

- `palace_search` 工具行为不变。
- 不引入 builtin 的 query 关键词过滤(已决定 builtin 不进预取)。
- 不改 system prompt 的构建时机(中途刷新另议)。
- `agent.go:615` 的压缩洞察 TODO、会话恢复等其他缺口不在本次。
