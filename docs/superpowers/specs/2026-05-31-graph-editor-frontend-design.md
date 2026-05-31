# 浏览器图编辑器设计(子项目 B)

**日期**: 2026-05-31
**分支**: feat/graph-orchestrator
**关联**: 浏览器图工作流编辑器(档 3)的前端半。后端半见 `docs/superpowers/specs/2026-05-31-graph-editor-backend-design.md`(子项目 A,已实现于 `pkg/grapheditor` + `cmd/hermes-editor`)。B 消费 A 的 API。

## 背景与目标

子项目 A 提供了 dev-only 本地 HTTP 服务:`GET /api/nodetypes`(反射导出各节点类型的字段 schema)、`POST /api/validate`(校验图 JSON),并从内嵌 `pkg/grapheditor/static/` 同源托管前端。本子项目实现那个前端:一个浏览器内的可视化图工作流编辑器。

**目标**
- 拖拽式可视化编辑 hermes 的 Graph:增删节点、连边(带优先级+条件)、按 schema 编辑节点配置。
- 校验当前图并把错误既显示在画布(可定位的)也汇总到面板(全部)。
- 导入(粘贴/上传 JSON)与导出(下载 JSON);后端不落盘,图活在浏览器会话。

**非目标(YAGNI)**
- 不做图执行/运行预览(那需要后端新增 run API,超出档 3 当前范围)。
- 不持久化节点坐标(Graph 模型无坐标字段)。
- 不引入额外状态管理库(用 React Flow 内置状态 + React context)。
- 不做多图库管理、鉴权、协作。
- v1 不做 Playwright e2e(Vitest 单测 + 手动浏览器验证即可)。

## 核心决策

| 决策点 | 结论 |
|---|---|
| v1 能力 | 完整编辑器(增删/连边/配置/校验/导入导出) |
| 技术栈 | Vite + React + TypeScript + React Flow v12 |
| 状态管理 | React Flow `useNodesState`/`useEdgesState` + React context(选择/校验/schema),不引 Zustand |
| 源码位置 | 新 `web/`(独立 npm 包) |
| 构建集成 | `npm run build` 输出到 `pkg/grapheditor/static/` 并提交;`go run ./cmd/hermes-editor` 开箱即用 |
| 开发工作流 | `npm run dev` 起 Vite,代理 `/api` → `127.0.0.1:7390` |
| 布局 | 三栏:顶部工具栏 + 左 Palette + 中 Canvas + 右 Inspector |
| 节点外观 | 标准式(id + type + 单个关键字段摘要),颜色编码类型,内联错误徽标 |
| 校验展示 | 画布徽标(可定位)+ 完整 ErrorPanel(点行聚焦);`<graph>` 错误仅进面板 |
| 节点位置 | dagre 自动布局,坐标不持久、不导出 |

## 项目布局

```
web/
  package.json
  vite.config.ts            # base "/"; build.outDir = ../pkg/grapheditor/static; /api 代理到 :7390
  tsconfig.json
  index.html
  src/
    main.tsx
    App.tsx                 # 布局壳 + 顶部工具栏(Import/Export/Validate/错误计数)
    api/client.ts           # getNodeTypes(): NodeTypeSchema[]; validate(graph): ValidateResponse
    model/types.ts          # 前端类型 + 后端 wire 类型(NodeTypeSchema/FieldSchema/ValidateResponse)
    model/graph.ts          # 画布(RF nodes/edges)⇄ 后端 Graph JSON(PascalCase)双向转换
    model/layout.ts         # dagre 自动布局:给无坐标的节点排版
    model/errors.ts         # ValidationError.path → 画布元素 / 面板项 的映射
    state/EditorContext.tsx # 选择态、校验结果、schema 缓存
    components/Palette.tsx
    components/Canvas.tsx
    components/NodeCard.tsx
    components/Inspector.tsx
    components/ConfigForm.tsx
    components/EdgeForm.tsx
    components/ErrorPanel.tsx
    components/ImportDialog.tsx
    components/fields/{StringField,NumberField,BoolField,StringListField,RawJsonField}.tsx
Makefile (或 scripts/build-ui.sh)   # 目标 editor-ui: cd web && npm ci && npm run build
```

`vite.config.ts` 要点:`build.outDir` 指向 `../pkg/grapheditor/static`、`build.emptyOutDir: true`(覆盖占位 index.html);dev `server.proxy['/api'] = 'http://127.0.0.1:7390'`。

## 架构与组件

每个组件单一职责、接口清晰:

- **App** — 三栏布局壳;挂载时调一次 `getNodeTypes()`,把 schema 注入 context;持有工具栏动作(Import/Export/Validate)。
- **Palette** — 列出节点类型(来自 schema),每项可拖;拖到画布创建该类型的空配置节点。
- **Canvas** — React Flow 包装;渲染 nodes/edges;处理拖拽连边、节点拖动、选择、缩放;用自定义节点类型 NodeCard。
- **NodeCard** — 自定义 RF 节点:彩色头(id + type)、一行关键字段摘要(llm→Model、tool→Resource、choice→分支数、end→Status,其余取第一个有值的标量字段)、有错误时右上角红徽标、连接 handle。
- **Inspector** — 上下文敏感:选中节点显示 ConfigForm + 节点 id 编辑;选中边显示 EdgeForm;无选择显示提示。
- **ConfigForm** — 按该节点类型的 schema 字段逐个渲染字段控件;值变更回写节点 config。
- **EdgeForm** — From/To(只读)、Priority(数字)、Condition(文本 + DSL 提示文案)。
- **fields/** — 按 FieldSchema.type 选控件:`string`→StringField(文本)、`number`→NumberField、`bool`→BoolField(勾选)、`string[]`→StringListField(标签增删)、`raw`→RawJsonField(JSON 文本框,失焦解析校验,非法标红并阻止导出/校验前置提示)。
- **ErrorPanel** — 列出 ValidateResponse.errors;每行显示 path + message;可定位项点击聚焦/居中对应节点或边,`<graph>` 项仅高亮该行。
- **ImportDialog** — 粘贴 JSON 或上传 `.json`;客户端 `JSON.parse`,失败就地报错;成功则交给导入流程。

## 数据流

1. **加载**:`GET /api/nodetypes` → context 缓存 schema → Palette 与 ConfigForm 使用。
2. **编辑**:Palette 拖放创建节点(默认空配置,id 自动生成如 `node_1`);画布连边产生 `EdgeSpec`(默认 `Priority:0`、空 `Condition`);选中元素驱动 Inspector;表单变更回写 RF 状态。
3. **校验**:`model/graph.ts` 序列化当前图 → `POST /api/validate` → `model/errors.ts` 映射 → NodeCard 徽标 + ErrorPanel;工具栏错误计数更新。
4. **导出**:序列化为 PascalCase Graph JSON(丢弃坐标)→ 下载 `.json`。
5. **导入**:ImportDialog 取得 JSON → `model/graph.ts` 反序列化 → `model/layout.ts`(dagre)赋坐标 → 灌入 RF 状态。

### 转换契约(`model/graph.ts`)

唯一处理 PascalCase 的地方。后端 wire 形状(来自子项目 A 合约):

- 图:`{ StartAt, Nodes: {id: NodeSpec}, Edges: [EdgeSpec], MaxSteps?, ... }`。
- `NodeSpec`:`{ Type, Config }`(`Config` 为该类型的配置对象,键为 PascalCase 如 `Model`/`Resource`)。
- `EdgeSpec`:`{ From, To, Condition?, Priority, Label? }`。
- `raw` 字段(`OutputSchema`/`Parameters`/`Choices`/`Branches`/`Schema`)在前端以原始 JSON 值保存,转换时原样放入 Config。

画布内部结构:RF node `{ id, type, position, data: {config} }`、RF edge `{ id, source, target, data: {priority, condition} }`。`StartAt` 在前端以一个布尔/标记表示(某节点被标为起点);导出时取该节点 id 写入 `StartAt`。

## 错误处理

| 场景 | 行为 |
|---|---|
| 导入的文本非合法 JSON | ImportDialog 就地报错,不进画布 |
| `raw` 字段输入非法 JSON | 该字段标红;Validate/Export 前提示存在未解析字段 |
| `/api/validate` 返回 `valid:false` | 正常业务结果:渲染 errors(非 HTTP 错) |
| API 不可达 / 非 200(非校验失败) | 顶部 toast 提示 |
| 校验错误 path 为 `<graph>` 或无法映射 | 仅在 ErrorPanel 显示,不挂画布徽标 |

## 测试计划

- **Vitest 单测(纯逻辑,高价值)**:
  - `model/graph.ts`:构造一张含 llm/tool/choice/end + 边(带 priority/condition)的图,断言 RF→wire→RF 往返不丢信息、键为 PascalCase、`raw` 字段原样保留、`StartAt` 正确。
  - `model/layout.ts`:对无坐标节点排版后每个节点有有限的 x/y 且不重叠(基本断言)。
  - `model/errors.ts`:`edges[1].to` / `StartAt` 映射到具体元素;`<graph>` 映射为仅面板项。
  - `api/client.ts`:mock fetch,断言 getNodeTypes/validate 的请求与解析。
- **轻量组件测(React Testing Library)**:
  - ConfigForm:给定一个 schema(含各 type)渲染出对应控件;`raw` 字段输入非法 JSON 后标红。
  - ErrorPanel:点击可定位行触发聚焦回调。
- **手动浏览器验证**:`go run ./cmd/hermes-editor`(:7390)+ `cd web && npm run dev`,走 新建→连边→配置→Validate(看徽标+面板)→Export→Import 全流程;再 `npm run build` 后用纯 `cmd/hermes-editor` 验证内嵌产物可用。
- 前端测试与 Go 的 `-race` 限制无关。

## 成功标准

1. 能从空白或导入的 JSON 出发,拖拽搭出含全部 6 种节点的图并连边设优先级/条件。
2. ConfigForm 按 schema 正确渲染五类字段控件,`raw` 字段以 JSON 文本框编辑。
3. Validate 把可定位错误显示为画布徽标 + 面板项,`<graph>` 错误进面板;计数正确。
4. Export 下载的 JSON 能被 `POST /api/validate` 判为合法(往返自洽),键为 PascalCase 且不含坐标。
5. `npm run build` 产物提交进 `pkg/grapheditor/static/` 后,纯 `cmd/hermes-editor` 能托管出真正的编辑器。
6. Vitest 单测与轻量组件测全绿;`go build ./...` 仍绿(内嵌新产物)。

## 改动文件清单(高层)

| 动作 | 路径 |
|---|---|
| 新增 | `web/`(整套前端源码与配置,见“项目布局”) |
| 新增 | `Makefile` 或 `scripts/build-ui.sh`(editor-ui 构建目标) |
| 替换 | `pkg/grapheditor/static/`(构建产物覆盖占位 index.html,提交) |
| 可能微调 | `.gitignore`(忽略 `web/node_modules`、`web/dist` 若有) |
