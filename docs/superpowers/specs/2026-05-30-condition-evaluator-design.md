# 条件求值器设计 (Condition Evaluator)

**日期**: 2026-05-30
**分支**: feat/graph-orchestrator
**关联问题**: #2(条件表达式能力弱)、#3(边路由只看优先级,忽略 `EdgeSpec.Condition`)

## 背景

当前 orchestrator 的条件能力存在两个问题:

1. **choice 节点表达力弱且有 bug**:`pkg/orchestrator/runner/choice.go` 的 `evaluateCondition`
   只支持 `map[string]interface{}` 等值匹配,且对 `interface{}` 直接用 `!=` 比较 ——
   JSON 数字解析为 `float64`,运行时输入常为 `int`,导致 `float64(3) != int(3)` 永远不匹配。
2. **边路由忽略条件**:`pkg/orchestrator/graph.go` 的 `EdgeSpec` 定义了 `Condition` 字段,
   但 `pkg/orchestrator/executor/route.go` 的 `Route` 从不读取它,只按 `Priority` 排序取第一条边。
   用户在边上写的条件被静默忽略。

LangGraph 用任意 Python 函数做 conditional edges;Go 无 eval,需要另一条路径。

## 目标与非目标

**目标**
- 统一 choice 节点与边路由的条件求值逻辑,消除上述两个问题。
- 求值器抽象**可插拔**:将来可无缝替换为 `expr-lang/expr` 等成熟引擎,
  且**不改动调用点、不改动条件字符串格式**。
- 先落地一个零第三方依赖的轻量默认实现,覆盖绝大多数路由场景。

**非目标(YAGNI,本次不做)**
- membership(`in`)、算术运算、字符串函数。
- 对话历史 / scratchpad 作用域。
- per-graph 求值器注入(将来真需要时通过 `ExecutionContext` 下放)。

## 核心决策

| 决策点 | 结论 |
|---|---|
| 表达力档位 | 轻量(比较 + 逻辑),但接口通用,可后续替换引擎 |
| 求值作用域 | `input`(上一节点 Output)+ `state`(跨节点 WorkMem.State) |
| 条件语法 | **字符串表达式**(如 `"input.has_tool_calls == true"`) |
| 向后兼容 | **干净切换** —— 移除旧 map 等值语法,重写默认图与测试 |
| 边路由语义 | `result.Next` > 按 Priority 依次试 Condition、首中者胜 > 无条件边兜底 |
| 默认实现 | Go 标准库 `go/parser` 解析,遍历求值受支持的 AST 子集 |

字符串表达式而非结构化 JSON 的理由:`expr-lang` 这类引擎吃的就是字符串,
现在用字符串则将来换引擎时同一条字符串直接透传,契约不变。

## 架构

### 新增包 `pkg/orchestrator/condition`

```go
// Scope 是条件求值的作用域契约 —— 换引擎时此契约保持不变。
type Scope struct {
    Input interface{}            // 上一节点 Output(= ec.WorkMem.LastResult)
    State map[string]interface{} // 跨节点 WorkMem.State
}

type Evaluator interface {
    Evaluate(expr string, scope Scope) (bool, error)
    Validate(expr string) error // 解析期校验,不求值
}

// 包级默认实现 + 可替换钩子(可插拔)
var Default Evaluator = newDSLEvaluator()

func Evaluate(expr string, s Scope) (bool, error) { return Default.Evaluate(expr, s) }
func Validate(expr string) error                  { return Default.Validate(expr) }
```

调用点只依赖 `condition.Evaluate` / `condition.Validate`。
替换引擎只需 `condition.Default = exprLangEvaluator{}`,调用点与条件字符串均不变。

### 默认 DSL 实现(基于 `go/parser`)

`parser.ParseExpr(expr)` 返回 Go AST,免费获得分词、运算符优先级、括号嵌套,
parser bug 面几乎为零且零第三方依赖。求值器只遍历受支持的 AST 节点子集:

- **取值**:`input.x`、`state.y`、嵌套 `input.a.b`(`*ast.SelectorExpr` / `*ast.Ident`)
- **字面量**:bool / number / string(`*ast.BasicLit`、预声明 `true`/`false` `*ast.Ident`)
- **比较**:`==` `!=` `>` `<` `>=` `<=`(数值跨类型按 `float64` 归一,复用 `toFloat64`)
- **逻辑**:`&&` `||` `!` + 括号(`*ast.BinaryExpr` / `*ast.UnaryExpr` / `*ast.ParenExpr`)

> 因走 `go/parser`,逻辑运算用 `&&`/`||`/`!`,而非 `and`/`or`/`not`。

不支持的节点(函数调用、算术等)在遍历时报错,由 `Validate` 在加载期暴露。

**取值语义**:`input` / `state` 解析为对应 map;`SelectorExpr` 逐层做 map 索引。
取值缺失(字段不存在)→ 求值为「未定义」,与任何值比较结果为 `false`,**不报错**,保证路由健壮。

## 集成

### 边路由 `route.go`(实现 #3)

```
1. result.Next != ""        → 返回(动态覆盖,最高优先级)
2. 收集出边,按 Priority 升序
3. 依次遍历:
     边无 Condition          → 无条件通过,立即返回(其优先级位即"首中者")
     边有 Condition          → condition.Evaluate 通过则返回
4. 无人通过                  → error("no matching edge from %q")
```

- Scope 来自 `result.Output`(Input)+ `ec.WorkMem.State`。
  `ec interface{}` 类型断言为 `*agcontext.ExecutionContext`;断言失败则退化为空 State。
- 作者约定:条件边放低 Priority 数(高优先级),无条件默认边放高 Priority 数(兜底)。
- 纯 Priority 旧图(全部无 Condition)行为不变 → 向后兼容。

### choice 节点 `choice.go`

- `ChoiceEntry.Condition` 由 `json.RawMessage` 改为 `string`。
- 用第 4 个参数 `execCtx`(现被忽略)断言出 `ec`,构造
  `Scope{Input: input, State: ec.WorkMem.State}`,调 `condition.Evaluate`。
- 移除 `evaluateCondition` / `valuesEqual`;`toFloat64` 迁入 `condition` 包。

### `graph.go`

- `EdgeSpec.Condition` 由 `json.RawMessage` 改为 `string`。

### 加载期校验(与 HERMES_GRAPH 协同)

`UnmarshalGraph` 后新增 `Graph.Validate()`,遍历所有 choice 分支条件 + 边条件,
逐个调 `condition.Validate(expr)`。`HERMES_GRAPH` 传入的自定义图若条件写错,**加载即报错**,
而非跑到该路径才失败。

### 默认图 `graph_builder.go`

```json
"Choices": [
  {"Condition": "input.has_tool_calls == true",   "Next": "dispatch_tools"},
  {"Condition": "input.needs_compression == true", "Next": "compress"}
]
```

## 错误处理

| 场景 | 行为 |
|---|---|
| 解析错误(语法/不支持的节点) | `Validate` 在加载期拦截,fail fast |
| 运行期取值缺失 | 求值为 `false`,不报错 |
| 运行期解析错误(理论上已被加载期挡掉) | 作为路由 error 冒泡 |

## 测试计划(TDD)

- **`condition` 包**:表驱动覆盖每个运算符、字段/嵌套取值、数值跨类型、逻辑优先级、
  缺失字段 → `false`、解析错误 → `Validate` 捕获。
- **choice runner**:现有测试改写为字符串语法。
- **route**:条件边选择、优先级排序、无条件兜底、`result.Next` 覆盖。
- **加载校验**:非法条件字符串 → `Graph.Validate` 报错。

## 文件清单

| 动作 | 文件 |
|---|---|
| 新增 | `pkg/orchestrator/condition/{evaluator.go, dsl.go, evaluator_test.go}` |
| 改 | `choice.go`、`route.go`、`graph.go`、`graph_builder.go` |
| 改测试 | `runner_test.go`、`route_test.go`、必要时 `agent_test.go` |
