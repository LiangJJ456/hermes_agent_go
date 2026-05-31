# 浏览器图编辑器 实现 Plan(子项目 B)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `web/` 实现一个 Vite + React + TS + React Flow 的浏览器图编辑器,构建产物内嵌进 `pkg/grapheditor/static/`,由 `cmd/hermes-editor` 同源托管,消费子项目 A 的 `/api/nodetypes` 与 `/api/validate`。

**Architecture:** 纯逻辑模块(model/、api/)走 Vitest TDD;UI 组件(React Flow 画布 + schema 驱动表单 + 错误面板)走 React Testing Library 轻量测试。状态用 React Flow 内置 hooks + 一个 React context。导入自动 dagre 排版,坐标不持久。

**Tech Stack:** React 19, TypeScript, Vite 8, `@xyflow/react` 12, `@dagrejs/dagre` 3, Vitest 4 + @testing-library/react(jsdom)。

**前置事实(来自子项目 A 合约,已实现):**
- `GET /api/nodetypes` → `[{type, fields:[{name, jsonName, type, optional}]}]`,`type ∈ {string,number,bool,string[],raw}`。
- `POST /api/validate`(body=图 JSON)→ `{valid, errors:[{path, message}]}`;invalid 仍 200;仅非 JSON body=400;`errors` 永远是数组。
- 校验 `path` 仅 `StartAt`、`edges[N].from/.to` 精确;其余收敛为 `<graph>`,定位信息在 `message` 文本(如 `node "x": ...`、`edge 0 (a->b): ...`)。
- 图 JSON 键为 **PascalCase**:`StartAt/Nodes/Edges`,`NodeSpec{Type,Config}`,`EdgeSpec{From,To,Condition,Priority,Label}`;`raw` 字段(`OutputSchema/Parameters/Choices/Branches/Schema`)为原始 JSON 值。Graph 无坐标字段。
- 静态资源从内嵌 `pkg/grapheditor/static/` 同源托管(`go:embed static`,目录需非空)。

**工作目录约定:** 除特别说明,命令在 `web/` 下运行。模块根 = `C:\Users\galaxy\code\hermes_agent_go`。

---

### Task 1: 脚手架 web/ + Vite + Vitest + 构建集成

**Files:**
- Create: `web/package.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/index.html`, `web/.gitignore`
- Create: `web/src/main.tsx`, `web/src/App.tsx`, `web/src/test/setup.ts`
- Test: `web/src/smoke.test.ts`

- [ ] **Step 1: 写 package.json**

Create `web/package.json`:

```json
{
  "name": "hermes-graph-editor",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc -b && vite build",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "dependencies": {
    "@dagrejs/dagre": "^3.0.0",
    "@xyflow/react": "^12.10.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.4.0",
    "@testing-library/react": "^16.1.0",
    "@testing-library/user-event": "^14.5.0",
    "@types/react": "^19.0.0",
    "@types/react-dom": "^19.0.0",
    "@vitejs/plugin-react": "^5.0.0",
    "jsdom": "^25.0.0",
    "typescript": "^5.6.0",
    "vite": "^8.0.0",
    "vitest": "^4.0.0"
  }
}
```

- [ ] **Step 2: 写 tsconfig / vite.config / index.html / .gitignore**

Create `web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "useDefineForClassFields": true,
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "types": ["vitest/globals", "@testing-library/jest-dom"]
  },
  "include": ["src"]
}
```

Create `web/vite.config.ts`:

```ts
/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../pkg/grapheditor/static',
    emptyOutDir: true,
  },
  server: {
    proxy: { '/api': 'http://127.0.0.1:7390' },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
  },
});
```

Create `web/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Hermes Graph Editor</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

Create `web/.gitignore`:

```
node_modules
```

> 注:不忽略构建产物 —— 产物输出到 `../pkg/grapheditor/static/` 并提交(见 Task 16)。`web/` 下无 dist。

- [ ] **Step 3: 写 setup、main、App stub**

Create `web/src/test/setup.ts`:

```ts
import '@testing-library/jest-dom/vitest';
```

Create `web/src/App.tsx`:

```tsx
export default function App() {
  return <div>Hermes Graph Editor</div>;
}
```

Create `web/src/main.tsx`:

```tsx
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from './App';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
```

- [ ] **Step 4: 写 smoke 测试**

Create `web/src/smoke.test.ts`:

```ts
import { describe, it, expect } from 'vitest';

describe('toolchain', () => {
  it('runs vitest', () => {
    expect(1 + 1).toBe(2);
  });
});
```

- [ ] **Step 5: 安装依赖并跑测试(确认工具链可用)**

Run (in `web/`): `npm install` then `npm test`
Expected: `npm install` 成功;`npm test` 显示 `smoke.test.ts` 1 passed。

- [ ] **Step 6: 验证构建产物落到内嵌目录,且 go build 仍可嵌入**

Run (in `web/`): `npm run build`
Expected: 成功;`../pkg/grapheditor/static/` 现含 `index.html` + `assets/`(Vite 产物,覆盖了占位页)。
Run (in module root): `go build ./...`
Expected: 成功(`//go:embed static` 嵌入新产物)。

> 本 Task 不提交构建产物(留到 Task 16 统一提交)。若想保持工作树干净,可在本 Task 结束时 `git checkout -- pkg/grapheditor/static/index.html` 恢复占位页并删除 `pkg/grapheditor/static/assets`;Task 16 会重新构建并提交。

- [ ] **Step 7: 提交脚手架(不含构建产物)**

```bash
git add web/ && git commit -m "feat(web): scaffold Vite+React+TS+Vitest graph editor frontend"
```

---

### Task 2: model/types.ts — wire 类型与编辑器类型

**Files:**
- Create: `web/src/model/types.ts`
- Test: `web/src/model/types.test.ts`

- [ ] **Step 1: 写编译校验测试**

Create `web/src/model/types.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import type {
  FieldSchema,
  NodeTypeSchema,
  ValidateResponse,
  WireGraph,
  NodeData,
  EdgeData,
} from './types';

describe('types', () => {
  it('constructs each shape', () => {
    const f: FieldSchema = { name: 'Model', jsonName: 'Model', type: 'string', optional: false };
    const nt: NodeTypeSchema = { type: 'llm', fields: [f] };
    const vr: ValidateResponse = { valid: false, errors: [{ path: 'StartAt', message: 'x' }] };
    const g: WireGraph = { StartAt: 'a', Nodes: { a: { Type: 'end', Config: {} } }, Edges: [] };
    const nd: NodeData = { nodeType: 'llm', config: {}, isStart: true };
    const ed: EdgeData = { priority: 0, condition: '' };
    expect([nt, vr, g, nd, ed].length).toBe(5);
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- types`
Expected: 失败 —— 无法解析 `./types`。

- [ ] **Step 3: 实现 types.ts**

Create `web/src/model/types.ts`:

```ts
// --- Wire types: mirror the backend (sub-project A) JSON shapes. ---

export type FieldType = 'string' | 'number' | 'bool' | 'string[]' | 'raw';

export interface FieldSchema {
  name: string;
  jsonName: string;
  type: FieldType;
  optional: boolean;
}

export interface NodeTypeSchema {
  type: string;
  fields: FieldSchema[];
}

export interface ValidationError {
  path: string;
  message: string;
}

export interface ValidateResponse {
  valid: boolean;
  errors: ValidationError[];
}

// Graph JSON uses PascalCase keys (backend contract).
export interface WireNode {
  Type: string;
  Config?: Record<string, unknown>;
}

export interface WireEdge {
  From: string;
  To: string;
  Condition?: string;
  Priority: number;
  Label?: string;
}

export interface WireGraph {
  StartAt: string;
  Nodes: Record<string, WireNode>;
  Edges: WireEdge[];
  MaxSteps?: number;
}

// --- Editor (canvas) data carried on React Flow nodes/edges. ---
// Index signatures satisfy React Flow's Record<string, unknown> data constraint.

export interface NodeData {
  nodeType: string; // e.g. "llm"
  config: Record<string, unknown>; // PascalCase config values
  isStart: boolean;
  [key: string]: unknown;
}

export interface EdgeData {
  priority: number;
  condition: string;
  [key: string]: unknown;
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- types`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/model/types.ts web/src/model/types.test.ts
git commit -m "feat(web): wire and editor type definitions"
```

---

### Task 3: model/graph.ts — 画布 ⇄ wire 转换

**Files:**
- Create: `web/src/model/graph.ts`
- Test: `web/src/model/graph.test.ts`

- [ ] **Step 1: 写失败测试(往返自洽)**

Create `web/src/model/graph.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { toWire, fromWire } from './graph';
import type { WireGraph } from './types';

const wire: WireGraph = {
  StartAt: 'classify',
  Nodes: {
    classify: { Type: 'llm', Config: { Model: 'deepseek-v4', Tools: ['web.search'] } },
    route: { Type: 'choice', Config: { Choices: [{ Next: 'done', Condition: 'input.ok == true' }] } },
    done: { Type: 'end', Config: { Status: 'success' } },
  },
  Edges: [
    { From: 'classify', To: 'route', Priority: 0 },
    { From: 'route', To: 'done', Priority: 1, Condition: 'input.ok == true' },
  ],
};

describe('graph conversion', () => {
  it('fromWire produces canvas nodes/edges with PascalCase config preserved', () => {
    const { nodes, edges } = fromWire(wire);
    expect(nodes).toHaveLength(3);
    const classify = nodes.find((n) => n.id === 'classify')!;
    expect(classify.data.nodeType).toBe('llm');
    expect(classify.data.config.Model).toBe('deepseek-v4');
    expect(classify.data.isStart).toBe(true);
    expect(nodes.find((n) => n.id === 'route')!.data.isStart).toBe(false);
    expect(edges).toHaveLength(2);
    const e1 = edges.find((e) => e.source === 'route' && e.target === 'done')!;
    expect(e1.data!.priority).toBe(1);
    expect(e1.data!.condition).toBe('input.ok == true');
  });

  it('round-trips wire -> canvas -> wire without losing data', () => {
    const { nodes, edges } = fromWire(wire);
    const out = toWire(nodes, edges);
    expect(out.StartAt).toBe('classify');
    expect(Object.keys(out.Nodes).sort()).toEqual(['classify', 'done', 'route']);
    expect(out.Nodes.classify.Type).toBe('llm');
    expect(out.Nodes.classify.Config).toEqual({ Model: 'deepseek-v4', Tools: ['web.search'] });
    expect(out.Nodes.route.Config!.Choices).toEqual([{ Next: 'done', Condition: 'input.ok == true' }]);
    // edges compared as a set (order/id not significant)
    const edgeSet = out.Edges.map((e) => `${e.From}->${e.To}:${e.Priority}:${e.Condition ?? ''}`).sort();
    expect(edgeSet).toEqual(['classify->route:0:', 'route->done:1:input.ok == true']);
  });

  it('omits empty condition on export', () => {
    const { nodes, edges } = fromWire(wire);
    const out = toWire(nodes, edges);
    const e0 = out.Edges.find((e) => e.From === 'classify')!;
    expect('Condition' in e0).toBe(false);
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- graph`
Expected: 失败 —— 无法解析 `./graph` / 未定义 `toWire`/`fromWire`。

- [ ] **Step 3: 实现 graph.ts**

Create `web/src/model/graph.ts`:

```ts
import type { Node, Edge } from '@xyflow/react';
import type { WireGraph, WireNode, WireEdge, NodeData, EdgeData } from './types';

export type EditorNode = Node<NodeData>;
export type EditorEdge = Edge<EdgeData>;

// Canvas → wire (PascalCase). Positions are dropped (not part of the model).
export function toWire(nodes: EditorNode[], edges: EditorEdge[]): WireGraph {
  const Nodes: Record<string, WireNode> = {};
  let StartAt = '';
  for (const n of nodes) {
    Nodes[n.id] = { Type: n.data.nodeType, Config: n.data.config };
    if (n.data.isStart) StartAt = n.id;
  }
  const Edges: WireEdge[] = edges.map((e) => {
    const w: WireEdge = { From: e.source, To: e.target, Priority: e.data?.priority ?? 0 };
    if (e.data?.condition) w.Condition = e.data.condition;
    return w;
  });
  return { StartAt, Nodes, Edges };
}

// wire → canvas. Positions default to (0,0); autoLayout assigns real ones.
export function fromWire(g: WireGraph): { nodes: EditorNode[]; edges: EditorEdge[] } {
  const nodes: EditorNode[] = Object.entries(g.Nodes ?? {}).map(([id, n]) => ({
    id,
    type: 'hermes',
    position: { x: 0, y: 0 },
    data: { nodeType: n.Type, config: n.Config ?? {}, isStart: id === g.StartAt },
  }));
  const edges: EditorEdge[] = (g.Edges ?? []).map((e, i) => ({
    id: `e${i}-${e.From}-${e.To}`,
    source: e.From,
    target: e.To,
    data: { priority: e.Priority ?? 0, condition: e.Condition ?? '' },
  }));
  return { nodes, edges };
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- graph`
Expected: PASS(3 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/model/graph.ts web/src/model/graph.test.ts
git commit -m "feat(web): canvas <-> wire graph conversion"
```

---

### Task 4: model/layout.ts — dagre 自动布局

**Files:**
- Create: `web/src/model/layout.ts`
- Test: `web/src/model/layout.test.ts`

- [ ] **Step 1: 写失败测试**

Create `web/src/model/layout.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { autoLayout } from './layout';
import type { EditorNode, EditorEdge } from './graph';

const nodes: EditorNode[] = [
  { id: 'a', type: 'hermes', position: { x: 0, y: 0 }, data: { nodeType: 'llm', config: {}, isStart: true } },
  { id: 'b', type: 'hermes', position: { x: 0, y: 0 }, data: { nodeType: 'end', config: {}, isStart: false } },
];
const edges: EditorEdge[] = [
  { id: 'e0', source: 'a', target: 'b', data: { priority: 0, condition: '' } },
];

describe('autoLayout', () => {
  it('assigns finite, distinct positions', () => {
    const out = autoLayout(nodes, edges);
    expect(out).toHaveLength(2);
    for (const n of out) {
      expect(Number.isFinite(n.position.x)).toBe(true);
      expect(Number.isFinite(n.position.y)).toBe(true);
    }
    const a = out.find((n) => n.id === 'a')!;
    const b = out.find((n) => n.id === 'b')!;
    // left-to-right layout: downstream node placed further right
    expect(b.position.x).toBeGreaterThan(a.position.x);
  });

  it('preserves node data', () => {
    const out = autoLayout(nodes, edges);
    expect(out.find((n) => n.id === 'a')!.data.isStart).toBe(true);
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- layout`
Expected: 失败 —— 未定义 `autoLayout`。

- [ ] **Step 3: 实现 layout.ts**

Create `web/src/model/layout.ts`:

```ts
import dagre from '@dagrejs/dagre';
import type { EditorNode, EditorEdge } from './graph';

const NODE_W = 180;
const NODE_H = 64;

// Assign positions via a left-to-right dagre layout. React Flow positions are
// top-left corners; dagre returns node centers, so we offset by half-size.
export function autoLayout(nodes: EditorNode[], edges: EditorEdge[]): EditorNode[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: 'LR', nodesep: 40, ranksep: 90 });
  g.setDefaultEdgeLabel(() => ({}));

  for (const n of nodes) g.setNode(n.id, { width: NODE_W, height: NODE_H });
  for (const e of edges) {
    if (g.hasNode(e.source) && g.hasNode(e.target)) g.setEdge(e.source, e.target);
  }

  dagre.layout(g);

  return nodes.map((n) => {
    const p = g.node(n.id);
    return { ...n, position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 } };
  });
}
```

> 若 `@dagrejs/dagre` 缺类型声明导致 TS 报错,在 `web/src/dagre.d.ts` 加 `declare module '@dagrejs/dagre';`(v3 自带类型,通常无需)。

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- layout`
Expected: PASS(2 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/model/layout.ts web/src/model/layout.test.ts web/src/dagre.d.ts 2>/dev/null
git commit -m "feat(web): dagre auto-layout for imported graphs"
```

---

### Task 5: model/errors.ts — 校验错误 → 画布元素映射

**Files:**
- Create: `web/src/model/errors.ts`
- Test: `web/src/model/errors.test.ts`

- [ ] **Step 1: 写失败测试**

Create `web/src/model/errors.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { mapError } from './errors';

describe('mapError', () => {
  it('maps precise edge paths to that edge', () => {
    const m = mapError({ path: 'edges[2].to', message: 'edge 2: To references unknown node "ghost"' });
    expect(m.target).toEqual({ kind: 'edge', index: 2 });
  });

  it('best-effort maps "<graph>" node messages to the node', () => {
    const m = mapError({ path: '<graph>', message: 'node "search": validate config: resource required' });
    expect(m.target).toEqual({ kind: 'node', id: 'search' });
  });

  it('best-effort maps "<graph>" edge messages to the edge', () => {
    const m = mapError({ path: '<graph>', message: 'edge 0 (a->b): condition: ...' });
    expect(m.target).toEqual({ kind: 'edge', index: 0 });
  });

  it('falls back to graph for unlocatable errors', () => {
    const m = mapError({ path: 'StartAt', message: 'StartAt is empty' });
    expect(m.target).toEqual({ kind: 'graph' });
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- errors`
Expected: 失败 —— 未定义 `mapError`。

- [ ] **Step 3: 实现 errors.ts**

Create `web/src/model/errors.ts`:

```ts
import type { ValidationError } from './types';

export type ErrorTarget =
  | { kind: 'node'; id: string }
  | { kind: 'edge'; index: number }
  | { kind: 'graph' };

export interface MappedError {
  error: ValidationError;
  target: ErrorTarget;
}

// Map a validation error to a canvas element. Precise paths (edges[N].*) map
// directly. Parse-level errors arrive as path "<graph>" with the location only
// in the message text (backend contract), so we best-effort parse node/edge
// references from the message. Anything unlocatable maps to the graph (panel).
export function mapError(e: ValidationError): MappedError {
  const edgePath = e.path.match(/^edges\[(\d+)\]/);
  if (edgePath) return { error: e, target: { kind: 'edge', index: Number(edgePath[1]) } };

  const nodeMsg = e.message.match(/node "([^"]+)"/);
  if (nodeMsg) return { error: e, target: { kind: 'node', id: nodeMsg[1] } };

  const edgeMsg = e.message.match(/edge (\d+)/);
  if (edgeMsg) return { error: e, target: { kind: 'edge', index: Number(edgeMsg[1]) } };

  return { error: e, target: { kind: 'graph' } };
}
```

> 与 spec 的"<graph> 仅进面板"相比,这里对含 `node "x"`/`edge N` 文本的 `<graph>` 错误做了尽力映射(更有用,符合后端合约注记)。映射到的节点在画布不存在时,消费方(Task 12/15)回退为仅面板高亮。

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- errors`
Expected: PASS(4 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/model/errors.ts web/src/model/errors.test.ts
git commit -m "feat(web): map validation errors to canvas elements (best-effort)"
```

---

### Task 6: api/client.ts — 后端 API 客户端

**Files:**
- Create: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts`

- [ ] **Step 1: 写失败测试(mock fetch)**

Create `web/src/api/client.test.ts`:

```ts
import { describe, it, expect, vi, afterEach } from 'vitest';
import { getNodeTypes, validateGraph } from './client';
import type { WireGraph } from '../model/types';

afterEach(() => vi.unstubAllGlobals());

function mockFetch(status: number, body: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => ({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
    })),
  );
}

describe('api client', () => {
  it('getNodeTypes returns parsed schemas', async () => {
    mockFetch(200, [{ type: 'llm', fields: [] }]);
    const schemas = await getNodeTypes();
    expect(schemas[0].type).toBe('llm');
    expect(fetch).toHaveBeenCalledWith('/api/nodetypes');
  });

  it('validateGraph posts the graph and parses the response', async () => {
    mockFetch(200, { valid: true, errors: [] });
    const g: WireGraph = { StartAt: 'a', Nodes: {}, Edges: [] };
    const res = await validateGraph(g);
    expect(res.valid).toBe(true);
    const call = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(call[0]).toBe('/api/validate');
    expect(call[1].method).toBe('POST');
    expect(JSON.parse(call[1].body)).toEqual(g);
  });

  it('throws on non-ok (non-validation) HTTP status', async () => {
    mockFetch(500, {});
    await expect(getNodeTypes()).rejects.toThrow(/500/);
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- client`
Expected: 失败 —— 未定义 `getNodeTypes`/`validateGraph`。

- [ ] **Step 3: 实现 client.ts**

Create `web/src/api/client.ts`:

```ts
import type { NodeTypeSchema, ValidateResponse, WireGraph } from '../model/types';

export async function getNodeTypes(): Promise<NodeTypeSchema[]> {
  const r = await fetch('/api/nodetypes');
  if (!r.ok) throw new Error(`/api/nodetypes: HTTP ${r.status}`);
  return (await r.json()) as NodeTypeSchema[];
}

// Note: an invalid graph still returns HTTP 200 with valid:false (backend
// contract). Only transport / non-2xx (e.g. 400 non-JSON, 500) throws.
export async function validateGraph(graph: WireGraph): Promise<ValidateResponse> {
  const r = await fetch('/api/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(graph),
  });
  if (!r.ok) throw new Error(`/api/validate: HTTP ${r.status}`);
  return (await r.json()) as ValidateResponse;
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- client`
Expected: PASS(3 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/api/client.ts web/src/api/client.test.ts
git commit -m "feat(web): backend API client (nodetypes, validate)"
```

---

### Task 7: 字段控件 fields/*

**Files:**
- Create: `web/src/components/fields/StringField.tsx`, `NumberField.tsx`, `BoolField.tsx`, `StringListField.tsx`, `RawJsonField.tsx`
- Test: `web/src/components/fields/fields.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/components/fields/fields.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { StringField } from './StringField';
import { NumberField } from './NumberField';
import { BoolField } from './BoolField';
import { StringListField } from './StringListField';
import { RawJsonField } from './RawJsonField';

describe('field widgets', () => {
  it('StringField calls onChange with text', () => {
    const onChange = vi.fn();
    render(<StringField label="Model" value="x" onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'deepseek' } });
    expect(onChange).toHaveBeenCalledWith('deepseek');
  });

  it('NumberField calls onChange with a number', () => {
    const onChange = vi.fn();
    render(<NumberField label="Priority" value={0} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '3' } });
    expect(onChange).toHaveBeenCalledWith(3);
  });

  it('BoolField toggles', () => {
    const onChange = vi.fn();
    render(<BoolField label="Async" value={false} onChange={onChange} />);
    fireEvent.click(screen.getByLabelText('Async'));
    expect(onChange).toHaveBeenCalledWith(true);
  });

  it('StringListField adds an item', () => {
    const onChange = vi.fn();
    render(<StringListField label="Tools" value={['a']} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Tools add item'), { target: { value: 'b' } });
    fireEvent.click(screen.getByText('Add'));
    expect(onChange).toHaveBeenCalledWith(['a', 'b']);
  });

  it('RawJsonField reports valid parsed JSON on blur', () => {
    const onChange = vi.fn();
    render(<RawJsonField label="Parameters" value={{ a: 1 }} onChange={onChange} />);
    const ta = screen.getByLabelText('Parameters');
    fireEvent.change(ta, { target: { value: '{"b":2}' } });
    fireEvent.blur(ta);
    expect(onChange).toHaveBeenCalledWith({ b: 2 });
  });

  it('RawJsonField marks invalid JSON and does not call onChange', () => {
    const onChange = vi.fn();
    render(<RawJsonField label="Parameters" value={{}} onChange={onChange} />);
    const ta = screen.getByLabelText('Parameters');
    fireEvent.change(ta, { target: { value: '{bad' } });
    fireEvent.blur(ta);
    expect(onChange).not.toHaveBeenCalled();
    expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- fields`
Expected: 失败 —— 字段组件未定义。

- [ ] **Step 3: 实现五个字段控件**

Create `web/src/components/fields/StringField.tsx`:

```tsx
interface Props {
  label: string;
  value: string;
  onChange: (v: string) => void;
}
export function StringField({ label, value, onChange }: Props) {
  return (
    <label className="field">
      <span>{label}</span>
      <input aria-label={label} value={value ?? ''} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}
```

Create `web/src/components/fields/NumberField.tsx`:

```tsx
interface Props {
  label: string;
  value: number;
  onChange: (v: number) => void;
}
export function NumberField({ label, value, onChange }: Props) {
  return (
    <label className="field">
      <span>{label}</span>
      <input
        type="number"
        aria-label={label}
        value={Number.isFinite(value) ? value : 0}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </label>
  );
}
```

Create `web/src/components/fields/BoolField.tsx`:

```tsx
interface Props {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
}
export function BoolField({ label, value, onChange }: Props) {
  return (
    <label className="field field-bool">
      <input type="checkbox" aria-label={label} checked={!!value} onChange={(e) => onChange(e.target.checked)} />
      <span>{label}</span>
    </label>
  );
}
```

Create `web/src/components/fields/StringListField.tsx`:

```tsx
import { useState } from 'react';

interface Props {
  label: string;
  value: string[];
  onChange: (v: string[]) => void;
}
export function StringListField({ label, value, onChange }: Props) {
  const [draft, setDraft] = useState('');
  const items = value ?? [];
  return (
    <div className="field">
      <span>{label}</span>
      <ul className="taglist">
        {items.map((it, i) => (
          <li key={i}>
            {it}
            <button type="button" onClick={() => onChange(items.filter((_, j) => j !== i))}>
              ×
            </button>
          </li>
        ))}
      </ul>
      <div className="tagadd">
        <input aria-label={`${label} add item`} value={draft} onChange={(e) => setDraft(e.target.value)} />
        <button
          type="button"
          onClick={() => {
            if (draft.trim()) {
              onChange([...items, draft.trim()]);
              setDraft('');
            }
          }}
        >
          Add
        </button>
      </div>
    </div>
  );
}
```

Create `web/src/components/fields/RawJsonField.tsx`:

```tsx
import { useState } from 'react';

interface Props {
  label: string;
  value: unknown;
  onChange: (v: unknown) => void;
}
export function RawJsonField({ label, value, onChange }: Props) {
  const [text, setText] = useState(() => JSON.stringify(value ?? null, null, 2));
  const [error, setError] = useState<string | null>(null);

  function commit() {
    try {
      const parsed = JSON.parse(text);
      setError(null);
      onChange(parsed);
    } catch {
      setError('Invalid JSON');
    }
  }

  return (
    <div className="field">
      <span>{label}</span>
      <textarea aria-label={label} value={text} onChange={(e) => setText(e.target.value)} onBlur={commit} rows={4} />
      {error && <div className="field-error">{error}</div>}
    </div>
  );
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- fields`
Expected: PASS(6 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/fields/
git commit -m "feat(web): schema field widgets (string/number/bool/list/raw-json)"
```

---

### Task 8: ConfigForm 与 EdgeForm

**Files:**
- Create: `web/src/components/ConfigForm.tsx`, `web/src/components/EdgeForm.tsx`
- Test: `web/src/components/ConfigForm.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/components/ConfigForm.tsx`'s test `web/src/components/ConfigForm.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ConfigForm } from './ConfigForm';
import { EdgeForm } from './EdgeForm';
import type { NodeTypeSchema } from '../model/types';

const llm: NodeTypeSchema = {
  type: 'llm',
  fields: [
    { name: 'Model', jsonName: 'Model', type: 'string', optional: false },
    { name: 'Tools', jsonName: 'Tools', type: 'string[]', optional: true },
    { name: 'OutputSchema', jsonName: 'OutputSchema', type: 'raw', optional: true },
    { name: 'MaxTokens', jsonName: 'MaxTokens', type: 'number', optional: true },
  ],
};

describe('ConfigForm', () => {
  it('renders a widget per schema field', () => {
    render(<ConfigForm schema={llm} config={{ Model: 'x' }} onChange={() => {}} />);
    expect(screen.getByLabelText('Model')).toBeInTheDocument();
    expect(screen.getByLabelText('Tools add item')).toBeInTheDocument();
    expect(screen.getByLabelText('OutputSchema')).toBeInTheDocument(); // raw -> textarea
    expect(screen.getByLabelText('MaxTokens')).toBeInTheDocument();
  });

  it('writes a changed field back through onChange', () => {
    const onChange = vi.fn();
    render(<ConfigForm schema={llm} config={{ Model: 'x' }} onChange={onChange} />);
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'deepseek' } });
    expect(onChange).toHaveBeenCalledWith({ Model: 'deepseek' });
  });
});

describe('EdgeForm', () => {
  it('shows readonly endpoints and edits priority/condition', () => {
    const onChange = vi.fn();
    render(<EdgeForm from="a" to="b" priority={0} condition="" onChange={onChange} />);
    expect(screen.getByText('a → b')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '2' } });
    expect(onChange).toHaveBeenCalledWith({ priority: 2, condition: '' });
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- ConfigForm`
Expected: 失败 —— `ConfigForm`/`EdgeForm` 未定义。

- [ ] **Step 3: 实现 ConfigForm 与 EdgeForm**

Create `web/src/components/ConfigForm.tsx`:

```tsx
import type { NodeTypeSchema, FieldSchema } from '../model/types';
import { StringField } from './fields/StringField';
import { NumberField } from './fields/NumberField';
import { BoolField } from './fields/BoolField';
import { StringListField } from './fields/StringListField';
import { RawJsonField } from './fields/RawJsonField';

interface Props {
  schema: NodeTypeSchema;
  config: Record<string, unknown>;
  onChange: (config: Record<string, unknown>) => void;
}

export function ConfigForm({ schema, config, onChange }: Props) {
  const set = (key: string, v: unknown) => onChange({ ...config, [key]: v });

  return (
    <div className="configform">
      {schema.fields.map((f) => (
        <FieldWidget key={f.jsonName} field={f} value={config[f.jsonName]} onChange={(v) => set(f.jsonName, v)} />
      ))}
    </div>
  );
}

function FieldWidget({ field, value, onChange }: { field: FieldSchema; value: unknown; onChange: (v: unknown) => void }) {
  switch (field.type) {
    case 'string':
      return <StringField label={field.jsonName} value={(value as string) ?? ''} onChange={onChange} />;
    case 'number':
      return <NumberField label={field.jsonName} value={(value as number) ?? 0} onChange={onChange} />;
    case 'bool':
      return <BoolField label={field.jsonName} value={(value as boolean) ?? false} onChange={onChange} />;
    case 'string[]':
      return <StringListField label={field.jsonName} value={(value as string[]) ?? []} onChange={onChange} />;
    case 'raw':
    default:
      return <RawJsonField label={field.jsonName} value={value} onChange={onChange} />;
  }
}
```

Create `web/src/components/EdgeForm.tsx`:

```tsx
import { NumberField } from './fields/NumberField';
import { StringField } from './fields/StringField';

interface Props {
  from: string;
  to: string;
  priority: number;
  condition: string;
  onChange: (v: { priority: number; condition: string }) => void;
}

export function EdgeForm({ from, to, priority, condition, onChange }: Props) {
  return (
    <div className="edgeform">
      <div className="edge-endpoints">
        {from} → {to}
      </div>
      <NumberField label="Priority" value={priority} onChange={(p) => onChange({ priority: p, condition })} />
      <StringField label="Condition" value={condition} onChange={(c) => onChange({ priority, condition: c })} />
      <p className="hint">Condition DSL: e.g. <code>input.has_tool_calls == true</code>. Empty = unconditional.</p>
    </div>
  );
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- ConfigForm`
Expected: PASS(3 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/ConfigForm.tsx web/src/components/EdgeForm.tsx web/src/components/ConfigForm.test.tsx
git commit -m "feat(web): schema-driven ConfigForm and EdgeForm"
```

---

### Task 9: EditorContext — schema/选择/校验态

**Files:**
- Create: `web/src/state/EditorContext.tsx`
- Test: `web/src/state/EditorContext.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/state/EditorContext.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { EditorProvider, useEditor } from './EditorContext';

function Probe() {
  const { selection, setSelection } = useEditor();
  return (
    <div>
      <span data-testid="sel">{selection ? `${selection.kind}:${selection.id}` : 'none'}</span>
      <button onClick={() => setSelection({ kind: 'node', id: 'a' })}>select</button>
    </div>
  );
}

describe('EditorContext', () => {
  it('provides and updates selection', () => {
    render(
      <EditorProvider schemas={[]}>
        <Probe />
      </EditorProvider>,
    );
    expect(screen.getByTestId('sel').textContent).toBe('none');
    fireEvent.click(screen.getByText('select'));
    expect(screen.getByTestId('sel').textContent).toBe('node:a');
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- EditorContext`
Expected: 失败 —— 未定义 `EditorProvider`/`useEditor`。

- [ ] **Step 3: 实现 EditorContext**

Create `web/src/state/EditorContext.tsx`:

```tsx
import { createContext, useContext, useState, type ReactNode } from 'react';
import type { NodeTypeSchema, ValidationError } from '../model/types';

// Named EditorSelection (not Selection) to avoid shadowing the DOM global `Selection`.
export type EditorSelection = { kind: 'node'; id: string } | { kind: 'edge'; id: string } | null;

interface EditorState {
  schemas: NodeTypeSchema[];
  schemaFor: (nodeType: string) => NodeTypeSchema | undefined;
  selection: EditorSelection;
  setSelection: (s: EditorSelection) => void;
  errors: ValidationError[];
  setErrors: (e: ValidationError[]) => void;
}

const Ctx = createContext<EditorState | null>(null);

export function EditorProvider({ schemas, children }: { schemas: NodeTypeSchema[]; children: ReactNode }) {
  const [selection, setSelection] = useState<EditorSelection>(null);
  const [errors, setErrors] = useState<ValidationError[]>([]);
  const schemaFor = (t: string) => schemas.find((s) => s.type === t);
  return (
    <Ctx.Provider value={{ schemas, schemaFor, selection, setSelection, errors, setErrors }}>
      {children}
    </Ctx.Provider>
  );
}

export function useEditor(): EditorState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useEditor must be used within EditorProvider');
  return v;
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- EditorContext`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add web/src/state/EditorContext.tsx web/src/state/EditorContext.test.tsx
git commit -m "feat(web): editor context (schemas, selection, validation errors)"
```

---

### Task 10: NodeCard — 自定义 React Flow 节点

**Files:**
- Create: `web/src/components/NodeCard.tsx`
- Test: `web/src/components/NodeCard.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/components/NodeCard.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { NodeCard, summarize } from './NodeCard';
import type { NodeData } from '../model/types';

function renderNode(data: NodeData, hasError = false) {
  return render(
    <ReactFlowProvider>
      <NodeCard
        id="classify"
        data={{ ...data, _hasError: hasError }}
        selected={false}
        type="hermes"
        dragging={false}
        zIndex={0}
        isConnectable
        positionAbsoluteX={0}
        positionAbsoluteY={0}
      />
    </ReactFlowProvider>,
  );
}

describe('NodeCard', () => {
  it('shows id, type, and a key-field summary', () => {
    renderNode({ nodeType: 'llm', config: { Model: 'deepseek-v4' }, isStart: true });
    expect(screen.getByText('classify')).toBeInTheDocument();
    expect(screen.getByText('llm')).toBeInTheDocument();
    expect(screen.getByText(/deepseek-v4/)).toBeInTheDocument();
  });

  it('shows an error badge when flagged', () => {
    renderNode({ nodeType: 'tool', config: {}, isStart: false }, true);
    expect(screen.getByLabelText('has errors')).toBeInTheDocument();
  });
});

describe('summarize', () => {
  it('picks the key field per type', () => {
    expect(summarize('llm', { Model: 'm' })).toBe('m');
    expect(summarize('tool', { Resource: 'r' })).toBe('r');
    expect(summarize('end', { Status: 's' })).toBe('s');
    expect(summarize('choice', { Choices: [1, 2] })).toBe('2 branches');
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- NodeCard`
Expected: 失败 —— 未定义 `NodeCard`/`summarize`。

- [ ] **Step 3: 实现 NodeCard**

Create `web/src/components/NodeCard.tsx`:

```tsx
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { NodeData } from '../model/types';

const TYPE_COLORS: Record<string, string> = {
  llm: '#2563eb',
  tool: '#059669',
  choice: '#d97706',
  parallel: '#7c3aed',
  human: '#db2777',
  end: '#6b7280',
};

// One-line summary of the most important config field for this node type.
export function summarize(nodeType: string, config: Record<string, unknown>): string {
  switch (nodeType) {
    case 'llm':
      return String(config.Model ?? '');
    case 'tool':
      return String(config.Resource ?? '');
    case 'end':
      return String(config.Status ?? '');
    case 'choice': {
      const c = config.Choices;
      return Array.isArray(c) ? `${c.length} branches` : '';
    }
    case 'parallel': {
      const b = config.Branches;
      return Array.isArray(b) ? `${b.length} branches` : '';
    }
    default: {
      const first = Object.values(config).find((v) => typeof v === 'string');
      return first ? String(first) : '';
    }
  }
}

export function NodeCard({ id, data }: NodeProps) {
  const d = data as NodeData & { _hasError?: boolean };
  const color = TYPE_COLORS[d.nodeType] ?? '#6b7280';
  const summary = summarize(d.nodeType, d.config);
  return (
    <div className="nodecard" style={{ borderColor: color }}>
      <Handle type="target" position={Position.Left} />
      {d._hasError && (
        <span className="nodecard-badge" aria-label="has errors">
          !
        </span>
      )}
      <div className="nodecard-head" style={{ background: color }}>
        <span className="nodecard-id">{id}</span>
        <span className="nodecard-type">{d.nodeType}</span>
        {d.isStart && <span className="nodecard-start">▶</span>}
      </div>
      {summary && <div className="nodecard-summary">{summary}</div>}
      <Handle type="source" position={Position.Right} />
    </div>
  );
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- NodeCard`
Expected: PASS(3 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/NodeCard.tsx web/src/components/NodeCard.test.tsx
git commit -m "feat(web): custom React Flow node card with type color, summary, error badge"
```

---

### Task 11: Palette — 节点类型面板

**Files:**
- Create: `web/src/components/Palette.tsx`
- Test: `web/src/components/Palette.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/components/Palette.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Palette } from './Palette';
import type { NodeTypeSchema } from '../model/types';

const schemas: NodeTypeSchema[] = [
  { type: 'llm', fields: [] },
  { type: 'tool', fields: [] },
  { type: 'end', fields: [] },
];

describe('Palette', () => {
  it('lists one draggable item per node type', () => {
    render(<Palette schemas={schemas} />);
    for (const t of ['llm', 'tool', 'end']) {
      const item = screen.getByText(t);
      expect(item).toBeInTheDocument();
      expect(item.closest('[draggable="true"]')).not.toBeNull();
    }
  });

  it('sets the node type on dragstart', () => {
    render(<Palette schemas={schemas} />);
    const item = screen.getByText('llm').closest('[draggable="true"]')!;
    const setData = vi.fn();
    fireEvent.dragStart(item, { dataTransfer: { setData } });
    expect(setData).toHaveBeenCalledWith('application/hermes-node-type', 'llm');
  });
});

import { vi, fireEvent } from 'vitest';
```

> 把上面误置于末尾的 import 移到文件顶部:`import { describe, it, expect, vi } from 'vitest';` 与 `import { render, screen, fireEvent } from '@testing-library/react';`。

- [ ] **Step 2: 修正 import 顺序后运行,确认失败**

最终 `Palette.test.tsx` 顶部 import 应为:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { Palette } from './Palette';
import type { NodeTypeSchema } from '../model/types';
```

(删除文件底部那两行 import。)

Run (in `web/`): `npm test -- Palette`
Expected: 失败 —— 未定义 `Palette`。

- [ ] **Step 3: 实现 Palette**

Create `web/src/components/Palette.tsx`:

```tsx
import type { NodeTypeSchema } from '../model/types';

export const NODE_TYPE_MIME = 'application/hermes-node-type';

export function Palette({ schemas }: { schemas: NodeTypeSchema[] }) {
  return (
    <aside className="palette">
      <div className="palette-title">Nodes</div>
      {schemas.map((s) => (
        <div
          key={s.type}
          className="palette-item"
          draggable
          onDragStart={(e) => e.dataTransfer.setData(NODE_TYPE_MIME, s.type)}
        >
          {s.type}
        </div>
      ))}
    </aside>
  );
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- Palette`
Expected: PASS(2 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/Palette.tsx web/src/components/Palette.test.tsx
git commit -m "feat(web): node-type palette with drag source"
```

---

### Task 12: ErrorPanel — 校验结果面板

**Files:**
- Create: `web/src/components/ErrorPanel.tsx`
- Test: `web/src/components/ErrorPanel.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/components/ErrorPanel.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ErrorPanel } from './ErrorPanel';

const errors = [
  { path: 'edges[0].to', message: 'edge 0: To references unknown node "ghost"' },
  { path: '<graph>', message: 'edge 0 (a->b): bad condition' },
];

describe('ErrorPanel', () => {
  it('lists every error with path and message', () => {
    render(<ErrorPanel errors={errors} onFocus={() => {}} />);
    expect(screen.getByText(/unknown node "ghost"/)).toBeInTheDocument();
    expect(screen.getByText(/bad condition/)).toBeInTheDocument();
    expect(screen.getByText('edges[0].to')).toBeInTheDocument();
  });

  it('focuses the mapped element when a row is clicked', () => {
    const onFocus = vi.fn();
    render(<ErrorPanel errors={errors} onFocus={onFocus} />);
    fireEvent.click(screen.getByText(/unknown node "ghost"/));
    expect(onFocus).toHaveBeenCalledWith({ kind: 'edge', index: 0 });
  });

  it('renders nothing visible-ish when no errors', () => {
    render(<ErrorPanel errors={[]} onFocus={() => {}} />);
    expect(screen.getByText(/no errors/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- ErrorPanel`
Expected: 失败 —— 未定义 `ErrorPanel`。

- [ ] **Step 3: 实现 ErrorPanel**

Create `web/src/components/ErrorPanel.tsx`:

```tsx
import type { ValidationError } from '../model/types';
import { mapError, type ErrorTarget } from '../model/errors';

interface Props {
  errors: ValidationError[];
  onFocus: (target: ErrorTarget) => void;
}

export function ErrorPanel({ errors, onFocus }: Props) {
  if (errors.length === 0) {
    return <div className="errorpanel errorpanel-empty">No errors</div>;
  }
  return (
    <div className="errorpanel">
      <div className="errorpanel-title">Errors ({errors.length})</div>
      <ul>
        {errors.map((e, i) => {
          const m = mapError(e);
          return (
            <li key={i} className="errorrow" onClick={() => onFocus(m.target)}>
              <div className="errorrow-path">{e.path}</div>
              <div className="errorrow-msg">{e.message}</div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- ErrorPanel`
Expected: PASS(3 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/ErrorPanel.tsx web/src/components/ErrorPanel.test.tsx
git commit -m "feat(web): validation error panel with click-to-focus"
```

---

### Task 13: ImportDialog — 导入

**Files:**
- Create: `web/src/components/ImportDialog.tsx`
- Test: `web/src/components/ImportDialog.test.tsx`

- [ ] **Step 1: 写失败测试**

Create `web/src/components/ImportDialog.test.tsx`:

```tsx
import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { ImportDialog } from './ImportDialog';

describe('ImportDialog', () => {
  it('parses valid JSON and calls onImport with the object', () => {
    const onImport = vi.fn();
    render(<ImportDialog onImport={onImport} onClose={() => {}} />);
    fireEvent.change(screen.getByLabelText('Graph JSON'), {
      target: { value: '{"StartAt":"a","Nodes":{},"Edges":[]}' },
    });
    fireEvent.click(screen.getByText('Import'));
    expect(onImport).toHaveBeenCalledWith({ StartAt: 'a', Nodes: {}, Edges: [] });
  });

  it('shows an error and does not call onImport for invalid JSON', () => {
    const onImport = vi.fn();
    render(<ImportDialog onImport={onImport} onClose={() => {}} />);
    fireEvent.change(screen.getByLabelText('Graph JSON'), { target: { value: '{nope' } });
    fireEvent.click(screen.getByText('Import'));
    expect(onImport).not.toHaveBeenCalled();
    expect(screen.getByText(/invalid json/i)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- ImportDialog`
Expected: 失败 —— 未定义 `ImportDialog`。

- [ ] **Step 3: 实现 ImportDialog**

Create `web/src/components/ImportDialog.tsx`:

```tsx
import { useState } from 'react';
import type { WireGraph } from '../model/types';

interface Props {
  onImport: (graph: WireGraph) => void;
  onClose: () => void;
}

export function ImportDialog({ onImport, onClose }: Props) {
  const [text, setText] = useState('');
  const [error, setError] = useState<string | null>(null);

  function doImport() {
    try {
      const parsed = JSON.parse(text) as WireGraph;
      setError(null);
      onImport(parsed);
    } catch {
      setError('Invalid JSON');
    }
  }

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div className="modal" onClick={(e) => e.stopPropagation()}>
        <h3>Import graph</h3>
        <textarea
          aria-label="Graph JSON"
          value={text}
          onChange={(e) => setText(e.target.value)}
          rows={12}
          placeholder='{"StartAt": "...", "Nodes": {...}, "Edges": [...]}'
        />
        {error && <div className="field-error">{error}</div>}
        <div className="modal-actions">
          <button type="button" onClick={onClose}>
            Cancel
          </button>
          <button type="button" onClick={doImport}>
            Import
          </button>
        </div>
      </div>
    </div>
  );
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- ImportDialog`
Expected: PASS(2 用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/ImportDialog.tsx web/src/components/ImportDialog.test.tsx
git commit -m "feat(web): import dialog with client-side JSON parse"
```

---

### Task 14: Canvas — React Flow 画布包装

**Files:**
- Create: `web/src/components/Canvas.tsx`
- Test: `web/src/components/Canvas.test.tsx`

- [ ] **Step 1: 写失败测试(渲染冒烟)**

Create `web/src/components/Canvas.test.tsx`:

```tsx
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { Canvas } from './Canvas';
import type { EditorNode, EditorEdge } from '../model/graph';

const nodes: EditorNode[] = [
  { id: 'a', type: 'hermes', position: { x: 0, y: 0 }, data: { nodeType: 'llm', config: { Model: 'm' }, isStart: true } },
];
const edges: EditorEdge[] = [];

describe('Canvas', () => {
  it('renders a node from props', () => {
    render(
      <ReactFlowProvider>
        <Canvas
          nodes={nodes}
          edges={edges}
          onNodesChange={() => {}}
          onEdgesChange={() => {}}
          onConnect={() => {}}
          onDropNode={() => {}}
          onSelect={() => {}}
        />
      </ReactFlowProvider>,
    );
    expect(screen.getByText('a')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- Canvas`
Expected: 失败 —— 未定义 `Canvas`。

- [ ] **Step 3: 实现 Canvas**

Create `web/src/components/Canvas.tsx`:

```tsx
import { useCallback, useMemo } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  type Connection,
  type NodeChange,
  type EdgeChange,
  type OnSelectionChangeParams,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { NodeCard } from './NodeCard';
import { NODE_TYPE_MIME } from './Palette';
import type { EditorNode, EditorEdge } from '../model/graph';
import type { EditorSelection } from '../state/EditorContext';

interface Props {
  nodes: EditorNode[];
  edges: EditorEdge[];
  onNodesChange: (c: NodeChange[]) => void;
  onEdgesChange: (c: EdgeChange[]) => void;
  onConnect: (c: Connection) => void;
  onDropNode: (nodeType: string, position: { x: number; y: number }) => void;
  onSelect: (sel: EditorSelection) => void;
}

export function Canvas({ nodes, edges, onNodesChange, onEdgesChange, onConnect, onDropNode, onSelect }: Props) {
  const nodeTypes = useMemo(() => ({ hermes: NodeCard }), []);

  const onDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      const nodeType = e.dataTransfer.getData(NODE_TYPE_MIME);
      if (!nodeType) return;
      const bounds = (e.target as HTMLElement).getBoundingClientRect();
      onDropNode(nodeType, { x: e.clientX - bounds.left, y: e.clientY - bounds.top });
    },
    [onDropNode],
  );

  const onSelectionChange = useCallback(
    (p: OnSelectionChangeParams) => {
      if (p.nodes.length > 0) onSelect({ kind: 'node', id: p.nodes[0].id });
      else if (p.edges.length > 0) onSelect({ kind: 'edge', id: p.edges[0].id });
      else onSelect(null);
    },
    [onSelect],
  );

  return (
    <div className="canvas-wrap" onDrop={onDrop} onDragOver={(e) => e.preventDefault()}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onSelectionChange={onSelectionChange}
        fitView
      >
        <Background />
        <Controls />
      </ReactFlow>
    </div>
  );
}
```

> 若 jsdom 下 React Flow 因缺 ResizeObserver 报错,在 `web/src/test/setup.ts` 追加一个最小 polyfill:
> ```ts
> class RO { observe() {} unobserve() {} disconnect() {} }
> // @ts-expect-error jsdom lacks ResizeObserver
> globalThis.ResizeObserver = globalThis.ResizeObserver ?? RO;
> ```

- [ ] **Step 4: 运行测试,确认通过**

Run (in `web/`): `npm test -- Canvas`
Expected: PASS(若需则已加 ResizeObserver polyfill)。

- [ ] **Step 5: 提交**

```bash
git add web/src/components/Canvas.tsx web/src/components/Canvas.test.tsx web/src/test/setup.ts
git commit -m "feat(web): React Flow canvas wrapper (custom nodes, drop, selection)"
```

---

### Task 15: App — 装配工具栏与全流程

**Files:**
- Modify: `web/src/App.tsx`(替换 stub:thin loader + provider)
- Create: `web/src/EditorShell.tsx`(消费 EditorContext 的主壳)
- Create: `web/src/components/Inspector.tsx`
- Create: `web/src/App.css`
- Test: `web/src/App.test.tsx`

- [ ] **Step 1: 写失败测试(集成冒烟,mock API)**

Create `web/src/App.test.tsx`:

```tsx
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import App from './App';

beforeEach(() => {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      if (url === '/api/nodetypes') {
        return { ok: true, status: 200, json: async () => [{ type: 'llm', fields: [] }, { type: 'end', fields: [] }] };
      }
      return { ok: true, status: 200, json: async () => ({ valid: true, errors: [] }) };
    }),
  );
});
afterEach(() => vi.unstubAllGlobals());

describe('App', () => {
  it('loads node types into the palette and shows the toolbar', async () => {
    render(<App />);
    expect(screen.getByText('Import')).toBeInTheDocument();
    expect(screen.getByText('Export')).toBeInTheDocument();
    expect(screen.getByText('Validate')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('llm')).toBeInTheDocument());
  });

  it('runs validation and shows the result', async () => {
    render(<App />);
    await waitFor(() => screen.getByText('llm'));
    fireEvent.click(screen.getByText('Validate'));
    await waitFor(() => expect(screen.getByText(/no errors/i)).toBeInTheDocument());
  });
});
```

- [ ] **Step 2: 运行测试,确认失败**

Run (in `web/`): `npm test -- App`
Expected: 失败(当前 App 是 stub,无工具栏/palette)。

- [ ] **Step 3: 实现 App + 样式**

Replace `web/src/App.tsx` with:

```tsx
import { useEffect, useState } from 'react';
import './App.css';
import { getNodeTypes } from './api/client';
import type { NodeTypeSchema } from './model/types';
import { EditorProvider } from './state/EditorContext';
import { EditorShell } from './EditorShell';

// App is a thin loader + provider. State (selection/errors/schemas) lives in
// EditorContext; EditorShell (below the provider) consumes it.
export default function App() {
  const [schemas, setSchemas] = useState<NodeTypeSchema[]>([]);
  useEffect(() => {
    getNodeTypes()
      .then(setSchemas)
      .catch((e) => alert(`Failed to load node types: ${(e as Error).message}`));
  }, []);
  return (
    <EditorProvider schemas={schemas}>
      <EditorShell />
    </EditorProvider>
  );
}
```

Create `web/src/EditorShell.tsx`:

```tsx
import { useCallback, useState } from 'react';
import { ReactFlowProvider, useNodesState, useEdgesState, addEdge, applyNodeChanges, type Connection } from '@xyflow/react';
import { validateGraph } from './api/client';
import { toWire, fromWire, type EditorNode, type EditorEdge } from './model/graph';
import { autoLayout } from './model/layout';
import { mapError, type ErrorTarget } from './model/errors';
import type { WireGraph } from './model/types';
import { useEditor } from './state/EditorContext';
import { Palette } from './components/Palette';
import { Canvas } from './components/Canvas';
import { Inspector } from './components/Inspector';
import { ErrorPanel } from './components/ErrorPanel';
import { ImportDialog } from './components/ImportDialog';

let idSeq = 1;

export function EditorShell() {
  const { schemas, selection, setSelection, errors, setErrors } = useEditor();
  const [nodes, setNodes, onNodesChange] = useNodesState<EditorNode>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<EditorEdge>([]);
  const [importing, setImporting] = useState(false);

  const errorNodeIds = new Set(
    errors
      .map((e) => mapError(e).target)
      .filter((t): t is { kind: 'node'; id: string } => t.kind === 'node')
      .map((t) => t.id),
  );
  const decoratedNodes = nodes.map((n) => ({ ...n, data: { ...n.data, _hasError: errorNodeIds.has(n.id) } }));

  const onConnect = useCallback(
    (c: Connection) => setEdges((eds) => addEdge({ ...c, data: { priority: 0, condition: '' } }, eds)),
    [setEdges],
  );

  const onDropNode = useCallback(
    (nodeType: string, position: { x: number; y: number }) => {
      const id = `${nodeType}_${idSeq++}`;
      setNodes((nds) => [
        ...nds,
        { id, type: 'hermes', position, data: { nodeType, config: {}, isStart: nds.length === 0 } },
      ]);
    },
    [setNodes],
  );

  const doImport = useCallback(
    (g: WireGraph) => {
      const { nodes: n, edges: e } = fromWire(g);
      setNodes(autoLayout(n, e));
      setEdges(e);
      setErrors([]);
      setImporting(false);
    },
    [setNodes, setEdges, setErrors],
  );

  const doExport = useCallback(() => {
    const blob = new Blob([JSON.stringify(toWire(nodes, edges), null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = 'graph.json';
    a.click();
    URL.revokeObjectURL(url);
  }, [nodes, edges]);

  const doValidate = useCallback(async () => {
    try {
      const res = await validateGraph(toWire(nodes, edges));
      setErrors(res.errors);
    } catch (e) {
      alert(`Validate failed: ${(e as Error).message}`);
    }
  }, [nodes, edges, setErrors]);

  const focusTarget = useCallback(
    (t: ErrorTarget) => {
      if (t.kind === 'node') {
        setNodes((nds) =>
          applyNodeChanges(
            nds.map((n) => ({ type: 'select' as const, id: n.id, selected: n.id === t.id })),
            nds,
          ),
        );
        setSelection({ kind: 'node', id: t.id });
      } else if (t.kind === 'edge') {
        const e = edges[t.index];
        if (e) setSelection({ kind: 'edge', id: e.id });
      }
    },
    [edges, setNodes, setSelection],
  );

  const updateNodeData = useCallback(
    (id: string, patch: Partial<EditorNode['data']>) =>
      setNodes((nds) => nds.map((n) => (n.id === id ? { ...n, data: { ...n.data, ...patch } } : n))),
    [setNodes],
  );
  const updateEdgeData = useCallback(
    (id: string, patch: Partial<NonNullable<EditorEdge['data']>>) =>
      setEdges((eds) => eds.map((e) => (e.id === id ? { ...e, data: { ...e.data!, ...patch } } : e))),
    [setEdges],
  );

  return (
    <div className="app">
      <header className="toolbar">
        <strong>Hermes Graph Editor</strong>
        <button onClick={() => setImporting(true)}>Import</button>
        <button onClick={doExport}>Export</button>
        <button onClick={doValidate}>Validate</button>
        <span className={errors.length ? 'errcount errcount-bad' : 'errcount'}>{errors.length} errors</span>
      </header>
      <div className="main">
        <Palette schemas={schemas} />
        <ReactFlowProvider>
          <Canvas
            nodes={decoratedNodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onDropNode={onDropNode}
            onSelect={setSelection}
          />
        </ReactFlowProvider>
        <div className="rightpane">
          <Inspector
            selection={selection}
            nodes={nodes}
            edges={edges}
            schemas={schemas}
            onUpdateNode={updateNodeData}
            onUpdateEdge={updateEdgeData}
          />
          <ErrorPanel errors={errors} onFocus={focusTarget} />
        </div>
      </div>
      {importing && <ImportDialog onImport={doImport} onClose={() => setImporting(false)} />}
    </div>
  );
}
```

Create `web/src/components/Inspector.tsx`:

```tsx
import type { NodeTypeSchema } from '../model/types';
import type { EditorNode, EditorEdge } from '../model/graph';
import type { EditorSelection } from '../state/EditorContext';
import { ConfigForm } from './ConfigForm';
import { EdgeForm } from './EdgeForm';

interface Props {
  selection: EditorSelection;
  nodes: EditorNode[];
  edges: EditorEdge[];
  schemas: NodeTypeSchema[];
  onUpdateNode: (id: string, patch: Partial<EditorNode['data']>) => void;
  onUpdateEdge: (id: string, patch: Partial<NonNullable<EditorEdge['data']>>) => void;
}

export function Inspector({ selection, nodes, edges, schemas, onUpdateNode, onUpdateEdge }: Props) {
  if (!selection) return <div className="inspector inspector-empty">Select a node or edge</div>;

  if (selection.kind === 'node') {
    const node = nodes.find((n) => n.id === selection.id);
    if (!node) return <div className="inspector inspector-empty">Node not found</div>;
    const schema = schemas.find((s) => s.type === node.data.nodeType);
    return (
      <div className="inspector">
        <div className="inspector-title">{node.id} <em>({node.data.nodeType})</em></div>
        {schema ? (
          <ConfigForm schema={schema} config={node.data.config} onChange={(config) => onUpdateNode(node.id, { config })} />
        ) : (
          <p>No schema for "{node.data.nodeType}"</p>
        )}
      </div>
    );
  }

  const edge = edges.find((e) => e.id === selection.id);
  if (!edge) return <div className="inspector inspector-empty">Edge not found</div>;
  return (
    <div className="inspector">
      <div className="inspector-title">Edge</div>
      <EdgeForm
        from={edge.source}
        to={edge.target}
        priority={edge.data?.priority ?? 0}
        condition={edge.data?.condition ?? ''}
        onChange={(v) => onUpdateEdge(edge.id, v)}
      />
    </div>
  );
}
```

Create `web/src/App.css`:

```css
* { box-sizing: border-box; }
html, body, #root { height: 100%; margin: 0; font-family: system-ui, sans-serif; }
.app { display: flex; flex-direction: column; height: 100vh; }
.toolbar { display: flex; gap: 8px; align-items: center; padding: 8px 12px; border-bottom: 1px solid #ddd; }
.toolbar button { padding: 4px 12px; cursor: pointer; }
.errcount { margin-left: auto; color: #555; }
.errcount-bad { color: #dc2626; font-weight: 600; }
.main { display: flex; flex: 1; min-height: 0; }
.palette { width: 120px; border-right: 1px solid #ddd; padding: 8px; }
.palette-title { font-size: 12px; text-transform: uppercase; color: #888; margin-bottom: 6px; }
.palette-item { padding: 6px 8px; margin-bottom: 6px; border: 1px solid #ccc; border-radius: 6px; cursor: grab; background: #f7f7f7; }
.canvas-wrap { flex: 1; min-width: 0; }
.rightpane { width: 280px; border-left: 1px solid #ddd; display: flex; flex-direction: column; overflow: auto; }
.inspector, .errorpanel { padding: 10px; }
.inspector-title { font-weight: 600; margin-bottom: 8px; }
.inspector-empty, .errorpanel-empty { color: #999; padding: 10px; }
.field { display: flex; flex-direction: column; gap: 2px; margin-bottom: 8px; font-size: 13px; }
.field input, .field textarea { width: 100%; padding: 4px; }
.field-bool { flex-direction: row; align-items: center; gap: 6px; }
.field-error { color: #dc2626; font-size: 12px; }
.taglist { list-style: none; padding: 0; margin: 4px 0; display: flex; flex-wrap: wrap; gap: 4px; }
.taglist li { background: #eee; border-radius: 4px; padding: 2px 6px; }
.hint { font-size: 11px; color: #888; }
.errorpanel { border-top: 1px solid #eee; }
.errorrow { border-left: 3px solid #dc2626; background: #fef2f2; padding: 4px 8px; margin-bottom: 6px; cursor: pointer; }
.errorrow-path { font-size: 11px; color: #888; }
.nodecard { background: #fff; border: 2px solid #6b7280; border-radius: 8px; min-width: 120px; font-size: 12px; position: relative; }
.nodecard-head { color: #fff; padding: 4px 8px; border-radius: 6px 6px 0 0; display: flex; gap: 6px; align-items: center; }
.nodecard-id { font-weight: 600; }
.nodecard-type { opacity: 0.85; font-size: 10px; text-transform: uppercase; }
.nodecard-start { margin-left: auto; }
.nodecard-summary { padding: 4px 8px; color: #444; }
.nodecard-badge { position: absolute; top: -8px; right: -8px; background: #dc2626; color: #fff; border-radius: 50%; width: 18px; height: 18px; display: flex; align-items: center; justify-content: center; font-size: 11px; }
.modal-backdrop { position: fixed; inset: 0; background: rgba(0,0,0,0.4); display: flex; align-items: center; justify-content: center; }
.modal { background: #fff; padding: 16px; border-radius: 8px; width: 520px; max-width: 90vw; }
.modal textarea { width: 100%; font-family: monospace; }
.modal-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
```

- [ ] **Step 4: 运行全部测试,确认通过**

Run (in `web/`): `npm test`
Expected: 全部测试 PASS(含 App 集成两用例)。

- [ ] **Step 5: 提交**

```bash
git add web/src/App.tsx web/src/EditorShell.tsx web/src/App.css web/src/components/Inspector.tsx web/src/App.test.tsx
git commit -m "feat(web): assemble editor app (toolbar, panes, import/export/validate flow)"
```

---

### Task 16: 构建产物嵌入 + Makefile + go 验证

**Files:**
- Create: `Makefile`(模块根)或 `scripts/build-ui.sh`
- Modify/replace: `pkg/grapheditor/static/`(提交 Vite 构建产物)

- [ ] **Step 1: 加构建脚本**

Create `Makefile`(模块根;若已存在则追加目标):

```makefile
.PHONY: editor-ui
editor-ui:
	cd web && npm ci && npm run build
```

- [ ] **Step 2: 构建前端产物**

Run (module root): `make editor-ui`(或 `cd web && npm run build`)
Expected: 成功;`pkg/grapheditor/static/` 含 `index.html` + `assets/`(覆盖占位页)。

- [ ] **Step 3: 校验 go 嵌入 + 全量 Go 测试**

Run (module root): `go build ./...`
Expected: 成功(嵌入新产物)。
Run (module root): `go test ./pkg/grapheditor/...`
Expected: PASS(server 测试中 `GET /` 现返回真实编辑器 HTML;断言只检查 200 与 `Hermes Graph Editor` 文本,Vite 产物的 `<title>Hermes Graph Editor</title>` 与根节点满足)。

> 若 `TestHandler_ServesIndex` 因产物 HTML 不含字面 "Hermes Graph Editor" 文本而失败:Vite 的 `index.html` 含 `<title>Hermes Graph Editor</title>`,断言用的是 `strings.Contains(body, "Hermes Graph Editor")`,匹配 title 即可,无需改测试。

- [ ] **Step 4: 提交构建产物**

```bash
git add pkg/grapheditor/static/ Makefile
git commit -m "build(web): embed built graph editor into pkg/grapheditor/static"
```

---

### Task 17: 手动浏览器验证(端到端)

**Files:** 无(验证任务)

- [ ] **Step 1: 起后端 + 前端 dev**

Terminal A (module root): `go run ./cmd/hermes-editor`(监听 127.0.0.1:7390)
Terminal B (`web/`): `npm run dev`(Vite,默认 5173,`/api` 代理到 7390)

- [ ] **Step 2: 走全流程**

在浏览器打开 Vite 给出的地址(如 http://localhost:5173),验证:
1. 左侧 Palette 出现 6 种节点类型(llm/tool/choice/parallel/human/end)。
2. 拖一个 llm 和一个 end 到画布;第一个落下的节点为 start(▶)。
3. 从 llm 的右 handle 拖到 end 左 handle 连边;选中边→右侧 EdgeForm 可改 Priority/Condition。
4. 选中 llm→ConfigForm 出现 Model/Tools/OutputSchema(JSON 文本框)等;填 Model。
5. 点 Validate:若有问题,问题节点出现红徽标且 ErrorPanel 列出;点 ErrorPanel 行能聚焦。
6. 点 Export 下载 graph.json;再点 Import 粘贴回去,画布经 dagre 重新排版还原。

- [ ] **Step 3: 验证内嵌产物(无 dev server)**

停掉 Vite;直接访问 `go run ./cmd/hermes-editor` 的 http://127.0.0.1:7390/ —— 应加载到 Task 16 内嵌的真实编辑器(非占位页),且 `/api/nodetypes`、`/api/validate` 同源可用。

- [ ] **Step 4: (如有手动修正)提交**

```bash
git add -A && git commit -m "fix(web): address manual verification findings"
```
(无修正则跳过。)

---

## Self-Review

**1. Spec coverage:**
- v1 完整编辑器 → Tasks 7–15 ✅
- Vite+React+TS+React Flow、web/ 布局、构建产物入 static、dev 代理 → Tasks 1, 16 ✅
- 三栏布局 + 工具栏 → Task 15(App + App.css)✅
- 标准节点(id+type+摘要)+ 类型配色 + 错误徽标 → Task 10 ✅
- schema 驱动表单(5 类控件,raw=JSON 文本框)→ Tasks 7, 8 ✅
- 校验:画布徽标 + 完整面板 + 点行聚焦;`<graph>`/不可定位进面板 → Tasks 5, 12, 15 ✅
- dagre 自动布局、坐标不持久不导出 → Tasks 4, 3(toWire 丢坐标)✅
- PascalCase 边界仅在 graph.ts → Task 3 ✅
- 导入(粘贴/解析)/导出(下载)→ Tasks 13, 15 ✅
- API 客户端、`valid:false` 非 HTTP 错 → Task 6 ✅
- 测试:Vitest 逻辑 + RTL 组件 + 手动浏览器 → 各 Task + Task 17 ✅
- 状态用 RF hooks + context(不引 Zustand)→ Tasks 9, 15 ✅
- 上传 `.json` 导入:spec 提到"粘贴或上传"。**Plan 仅实现粘贴**(ImportDialog 文本域)。为 YAGNI 收敛,文件上传未实现 —— 见下方偏差说明。

**2. Placeholder scan:** 无 TBD/TODO;每个代码步骤含完整代码。Task 11 测试有一处"import 写在文件底部"的纠正说明,已在 Step 2 给出最终正确 import 块。✅

**3. Type consistency:** `EditorNode`/`EditorEdge`(graph.ts)、`NodeData`/`EdgeData`(types.ts)、`toWire`/`fromWire`、`autoLayout`、`mapError`/`ErrorTarget`、`getNodeTypes`/`validateGraph`、`NODE_TYPE_MIME`、`summarize`、`ConfigForm`/`EdgeForm`/`Inspector`/`Palette`/`Canvas`/`ErrorPanel`/`ImportDialog`、`EditorProvider`/`useEditor`/`EditorSelection` 在定义与使用处一致。选择类型统一为 `EditorSelection`(Task 9 导出),`Canvas.onSelect`、`Inspector.selection`、`EditorShell` 的 `setSelection`(来自 context)都引用它,避免与 DOM 全局 `Selection` 混淆。EditorContext(Task 9)由 `EditorShell`(Task 15,provider 之下)消费 schemas/selection/errors —— 非死代码;`App` 仅作 loader+provider。

**与 spec 的已知偏差(收敛/简化):**
1. **导入只支持粘贴 JSON,不支持上传 `.json` 文件**(spec 写”粘贴或上传”)。YAGNI:粘贴覆盖核心场景;文件上传可后续加(给 ImportDialog 加一个 `<input type=”file”>` 读取后填入文本域)。
2. 错误映射对 `<graph>` 中含 `node “x”`/`edge N` 文本者做尽力定位(优于 spec 的”仅面板”),更实用且符合后端合约注记。
