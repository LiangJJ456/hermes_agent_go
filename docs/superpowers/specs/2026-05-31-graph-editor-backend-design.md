# 图编辑器后端 API 设计(子项目 A)

**日期**: 2026-05-31
**分支**: feat/graph-orchestrator
**关联**: 浏览器图工作流编辑器(档 3)。整体拆为两个子项目:**A 后端编辑器 API(本 spec)** → **B 浏览器编辑器(TS/React + React Flow,后续 spec)**。A 是契约 + 可独立测试的地基,B 消费 A。

## 背景与目标

hermes 的图编排已支持 JSON 序列化的 `Graph`(`StartAt/Nodes/Edges/MaxSteps`),节点类型经全局 registry 注册(llm/tool/choice/parallel/human/end)。目前没有任何 HTTP server,也没有"列出全部节点类型 / 导出节点配置 schema"的能力。

本子项目提供一个 **dev-only 的本地 HTTP 服务**,为浏览器编辑器输出两样东西:
1. 各节点类型的配置字段 schema(让前端自动生成配置表单)。
2. 图的校验(复用既有 `UnmarshalGraph` + `Graph.Validate` + `condition.Validate`)。

**目标**
- 反射导出节点 config schema,零 per-type 维护、随结构体自动同步。
- 校验任意图 JSON 并返回带定位的错误列表。
- 一个独立二进制 `cmd/hermes-editor` 起本地服务并托管前端静态资源。

**非目标(YAGNI)**
- 不落盘:不读写图文件(`HERMES_GRAPH` 等)。图活在浏览器会话里;加载=粘贴/拖入 JSON,保存=浏览器下载 JSON。
- 不做多图库管理、不做鉴权、不做热重载。
- 不在本子项目里实现前端(属于子项目 B);A 阶段静态目录放占位 `index.html`。
- 不引入新的图执行路径,不改 condition 求值语义。

## 核心决策

| 决策点 | 结论 |
|---|---|
| 持久化模型 | 无文件,纯内存/会话;后端不碰磁盘 |
| schema 丰富度 | 反射自动导出 + 复杂/嵌套类型降级为 `raw`(原生 JSON 文本框) |
| 服务入口 | 独立二进制 `cmd/hermes-editor`(与 REPL 隔离) |
| 可复用核心 | 新包 `pkg/grapheditor`(纯逻辑,可 httptest) |
| 节点类型来源 | blank import `runner` 包触发 `init()` 注册 → registry |
| 校验失败语义 | 业务结果(200 + `valid:false`),非 HTTP 错误 |
| 监听地址 | bind `127.0.0.1`,默认 `-addr 127.0.0.1:7390` |

## 架构与包划分

```
pkg/orchestrator/registry.go     ← 新增公开函数 ListNodeTypes()
pkg/grapheditor/                 ← 新包(可复用核心)
  ├─ schema.go    反射 config 结构体 → []NodeTypeSchema
  ├─ validate.go  包装 UnmarshalGraph + Graph.Validate + condition.Validate
  └─ server.go    http.ServeMux:/api/nodetypes、/api/validate、静态资源
cmd/hermes-editor/main.go        ← 新二进制:-addr,装 mux,blank import runner,ListenAndServe
```

依赖方向:`grapheditor` → `orchestrator`(registry / Graph / UnmarshalGraph)、`orchestrator/runner`(取 ConfigPrototype 类型)、`orchestrator/condition`(校验)。无环。`cmd/hermes-editor` import `grapheditor` 与 blank import `runner`。

### registry 公开列举

`pkg/orchestrator/registry.go` 现有 `RegisterNodeType` / `LookupNodeType`,但 registry map 私有且无列举入口。新增:

```go
// ListNodeTypes returns the names of all registered node types, sorted.
func ListNodeTypes() []string
```

(排序保证 schema 输出稳定、测试可断言。)

## schema 导出(reflection)

```go
type NodeTypeSchema struct {
    Type   string        `json:"type"`   // "llm" / "choice" / ...
    Fields []FieldSchema `json:"fields"`
}

type FieldSchema struct {
    Name     string `json:"name"`      // Go 字段名
    JSONName string `json:"jsonName"`  // json tag 名(去掉 ,omitempty)
    Type     string `json:"type"`      // string|number|bool|string[]|raw
    Optional bool   `json:"optional"`  // json tag 含 ,omitempty 即为 true
}
```

对每个 `ListNodeTypes()` 的类型,取其注册的 ConfigPrototype(`LookupNodeType` 返回 entry 上的 prototype),`reflect` 走导出字段:

| Go 类型 | FieldSchema.Type |
|---|---|
| `string` | `string` |
| `int/int64/uint/float64` 等数值 | `number` |
| `bool` | `bool` |
| `[]string` | `string[]` |
| 其它(嵌套 `*Graph`、`map[string]interface{}`、`json.RawMessage`、`interface{}`、结构体切片如 `[]ChoiceEntry`) | `raw` |

`raw` 表示前端用原生 JSON 文本框编辑。无 description/enum(结构体当前无相应 tag);YAGNI,后续若需要再经自定义 tag 扩展(子项目 B 不依赖它)。

**响应**:`GET /api/nodetypes` → `200` + `[]NodeTypeSchema`(JSON 数组,按 type 名排序)。

## 校验 API

```
POST /api/validate
  请求体: 图的 JSON(与 HERMES_GRAPH 文件同结构)
  200 {"valid": true,  "errors": []}
  200 {"valid": false, "errors": [{"path": "...", "message": "..."}]}
  400 {"error": "..."}   ← 仅当 body 连合法 JSON 都不是
```

```go
type ValidationError struct {
    Path    string `json:"path"`    // 如 "nodes.classify.condition" / "edges[1].condition" / "<graph>"
    Message string `json:"message"`
}

type ValidateResponse struct {
    Valid  bool              `json:"valid"`
    Errors []ValidationError `json:"errors"`
}
```

校验链(复用既有逻辑):
1. `UnmarshalGraph(body)` —— 结构 + 各节点 config 解析(含 `Validate()` hook);失败归入 errors(path 尽力定位,无法定位用 `<graph>`)。
2. `Graph.Validate()` —— 起点存在、边/引用完整、节点引用闭合。
3. 条件表达式 —— 边的 `Condition` 与 choice 节点的条件经 `condition.Validate`(或既有 load-time 校验路径)逐条检查;失败的 path 指向该边/该 choice。

校验失败是业务结果,HTTP 仍 200,`valid:false`。仅 body 非 JSON / 读取失败才 400。

## server 与 cmd

- `pkg/grapheditor/server.go` 暴露构造 `http.Handler`(或 `*http.ServeMux`)的函数,注册:
  - `GET  /api/nodetypes`
  - `POST /api/validate`
  - `GET  /` 及静态资源 —— `go:embed` 一个目录(A 阶段仅占位 `index.html`,内容为"editor frontend not built yet";B 阶段填入真正构建产物)。
- 同源托管,无 CORS 问题。
- `cmd/hermes-editor/main.go`:`flag` 解析 `-addr`(默认 `127.0.0.1:7390`),blank import `_ "…/pkg/orchestrator/runner"` 触发注册,构造 handler,`http.ListenAndServe`。启动时打印监听地址。

## 数据流

```
浏览器 → GET /api/nodetypes → 建节点面板 + 每节点配置表单
用户编辑 → 拼出 Graph JSON → POST /api/validate → 内联标红错误
保存 → 浏览器下载 .json(后端不落盘)
```

## 错误处理

| 场景 | 行为 |
|---|---|
| `/api/validate` body 非 JSON | 400 + `{"error": ...}` |
| 图结构/条件非法 | 200 + `{"valid":false, "errors":[…]}` |
| 节点 config 反射遇到未知/复杂类型 | FieldSchema.Type = `raw`(不报错) |
| 未知路由 | 交给静态文件 handler(404 由其处理) |

## 测试计划(TDD)

- `pkg/grapheditor/schema_test.go`:
  - `llm` 含 `Model(string)`、`Tools(string[])`、`OutputSchema(raw)`、`Temperature(number)`、`MaxTokens(number)`。
  - `choice` 含 `Choices(raw)`、`Default(string,optional)`。
  - `tool` 含 `Resource(string)`、`Async(bool)`、`Timeout(number)`、`Parameters(raw)`。
  - `Optional` 正确反映 `,omitempty`。
  - `ListNodeTypes()` 返回排序后的全部 6 类型。
- `pkg/grapheditor/validate_test.go`:
  - 合法图 → `valid:true`、`errors` 空。
  - 坏条件(如 `input.x === 1`)→ `valid:false`,某 error 的 path 指向该条件。
  - 缺失起点 / 悬空边引用 → `valid:false` 且有对应 error。
  - 非 JSON body → 400。
- `pkg/grapheditor/server_test.go`:`httptest` 打 `GET /api/nodetypes`(200 + 非空数组)、`POST /api/validate`(各分支状态码与响应体)。
- 全量 `go build ./...`、`go vet`、`gofmt -l`、`go test ./...` 全绿(`-race` 本机无 CGO/gcc 跑不了,沿用既有约束)。

## 成功标准

1. `GET /api/nodetypes` 返回全部 6 种节点类型及其字段 schema,复杂字段降级为 `raw`。
2. `POST /api/validate` 对好/坏图分别返回 `valid:true/false`,坏图错误带可定位 path。
3. `cmd/hermes-editor` 能起服务、访问根路径见占位页、两个 API 端点可用。
4. 后端全程不读写磁盘上的图文件。
5. 全量构建/vet/gofmt/test 绿。

## 改动文件清单

| 动作 | 文件 |
|---|---|
| 改 | `pkg/orchestrator/registry.go`(新增 `ListNodeTypes()`) |
| 新增 | `pkg/grapheditor/schema.go` |
| 新增 | `pkg/grapheditor/validate.go` |
| 新增 | `pkg/grapheditor/server.go` + `go:embed` 占位静态目录(如 `pkg/grapheditor/static/index.html`) |
| 新增 | `cmd/hermes-editor/main.go` |
| 新增测试 | `pkg/grapheditor/{schema,validate,server}_test.go`,`pkg/orchestrator/registry_test.go`(若无则新增 ListNodeTypes 用例) |
