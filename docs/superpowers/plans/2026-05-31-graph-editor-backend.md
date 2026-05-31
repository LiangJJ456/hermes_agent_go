# 图编辑器后端 API 实现 Plan(子项目 A)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 提供一个 dev-only 本地 HTTP 服务,反射导出节点类型配置 schema 并校验图 JSON,由新二进制 `cmd/hermes-editor` 托管,供后续浏览器编辑器(子项目 B)消费。

**Architecture:** 新增 `orchestrator.ListNodeTypes()` 列举注册类型;新包 `pkg/grapheditor` 承载纯逻辑(反射 schema、校验、HTTP handler + 内嵌静态资源);新二进制 `cmd/hermes-editor` blank import `runner` 触发节点注册后起服务。后端不读写磁盘上的图文件。

**Tech Stack:** Go 标准库(`reflect`、`net/http`、`embed`、`io/fs`、`encoding/json`),复用 `orchestrator.UnmarshalGraph` 与 `condition.Validate`。

**关于 spec 的两处落地说明:**
- spec 写"复用既有 `Graph.Validate()`",但该方法不存在。结构校验(起点存在/边引用闭合)改在 `grapheditor` 内做(`Graph` 字段均已导出,YAGNI:目前仅编辑器需要),不新增 orchestrator 方法。
- `UnmarshalGraph` 在首个错误即返回(含边条件非法),故解析层错误的 `path` 为 `<graph>`,message 含其内置定位(如 `edge 0 (a->b): ...`);结构层错误(start/edge 引用)由 `grapheditor` 给出精确 `path`(如 `edges[0].to`)。

**模块路径:** `code.byted.org/ad_creative/hermes_agent_go`

---

### Task 1: `orchestrator.ListNodeTypes()` — 列举注册节点类型

**Files:**
- Modify: `pkg/orchestrator/registry.go`
- Test: `pkg/orchestrator/registry_test.go`(新增)

- [ ] **Step 1: 写失败测试**

Create `pkg/orchestrator/registry_test.go`:

```go
package orchestrator

import (
	"context"
	"testing"
)

type stubRunner struct{}

func (stubRunner) Run(ctx context.Context, node *NodeSpec, input interface{},
	execCtx interface{}) (*NodeResult, error) {
	return nil, nil
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestListNodeTypes_SortedAndComplete(t *testing.T) {
	RegisterNodeType("zeta", stubRunner{}, nil)
	RegisterNodeType("alpha", stubRunner{}, nil)

	got := ListNodeTypes()

	for i := 1; i < len(got); i++ {
		if got[i-1] > got[i] {
			t.Fatalf("ListNodeTypes not sorted: %v", got)
		}
	}
	if !containsStr(got, "alpha") || !containsStr(got, "zeta") {
		t.Fatalf("missing registered types: %v", got)
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `go test ./pkg/orchestrator/ -run TestListNodeTypes -v`
Expected: 编译失败 —— `undefined: ListNodeTypes`。

- [ ] **Step 3: 实现 `ListNodeTypes`**

In `pkg/orchestrator/registry.go`, 把 import 块改为含 `sort`:

```go
import (
	"fmt"
	"sort"
	"sync"
)
```

在文件末尾追加:

```go
// ListNodeTypes returns the names of all registered node types, sorted.
func ListNodeTypes() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `go test ./pkg/orchestrator/ -run TestListNodeTypes -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add pkg/orchestrator/registry.go pkg/orchestrator/registry_test.go
git commit -m "feat(orchestrator): add ListNodeTypes for registry enumeration"
```

---

### Task 2: `grapheditor` 反射导出节点 schema

**Files:**
- Create: `pkg/grapheditor/schema.go`
- Test: `pkg/grapheditor/schema_test.go`

- [ ] **Step 1: 写失败测试**

Create `pkg/grapheditor/schema_test.go`:

```go
package grapheditor

import (
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func findSchema(schemas []NodeTypeSchema, typ string) *NodeTypeSchema {
	for i := range schemas {
		if schemas[i].Type == typ {
			return &schemas[i]
		}
	}
	return nil
}

func findField(s *NodeTypeSchema, jsonName string) *FieldSchema {
	for i := range s.Fields {
		if s.Fields[i].JSONName == jsonName {
			return &s.Fields[i]
		}
	}
	return nil
}

func TestBuildNodeTypeSchemas_LLM(t *testing.T) {
	schemas := BuildNodeTypeSchemas()

	llm := findSchema(schemas, "llm")
	if llm == nil {
		t.Fatal("llm schema missing")
	}
	if f := findField(llm, "Model"); f == nil || f.Type != "string" {
		t.Fatalf("Model field wrong: %+v", f)
	}
	if f := findField(llm, "Tools"); f == nil || f.Type != "string[]" || !f.Optional {
		t.Fatalf("Tools field wrong: %+v", f)
	}
	if f := findField(llm, "OutputSchema"); f == nil || f.Type != "raw" {
		t.Fatalf("OutputSchema field wrong: %+v", f)
	}
	if f := findField(llm, "Temperature"); f == nil || f.Type != "number" {
		t.Fatalf("Temperature field wrong: %+v", f)
	}
}

func TestBuildNodeTypeSchemas_ToolAndChoice(t *testing.T) {
	schemas := BuildNodeTypeSchemas()

	tool := findSchema(schemas, "tool")
	if tool == nil {
		t.Fatal("tool schema missing")
	}
	if f := findField(tool, "Resource"); f == nil || f.Type != "string" {
		t.Fatalf("Resource field wrong: %+v", f)
	}
	if f := findField(tool, "Async"); f == nil || f.Type != "bool" {
		t.Fatalf("Async field wrong: %+v", f)
	}
	if f := findField(tool, "Timeout"); f == nil || f.Type != "number" {
		t.Fatalf("Timeout field wrong: %+v", f)
	}
	if f := findField(tool, "Parameters"); f == nil || f.Type != "raw" {
		t.Fatalf("Parameters field wrong: %+v", f)
	}

	choice := findSchema(schemas, "choice")
	if choice == nil {
		t.Fatal("choice schema missing")
	}
	if f := findField(choice, "Choices"); f == nil || f.Type != "raw" {
		t.Fatalf("Choices field wrong: %+v", f)
	}
	if f := findField(choice, "Default"); f == nil || f.Type != "string" || !f.Optional {
		t.Fatalf("Default field wrong: %+v", f)
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `go test ./pkg/grapheditor/ -run TestBuildNodeTypeSchemas -v`
Expected: 编译失败 —— `undefined: BuildNodeTypeSchemas` / `NodeTypeSchema` / `FieldSchema`。

- [ ] **Step 3: 实现 `schema.go`**

Create `pkg/grapheditor/schema.go`:

```go
package grapheditor

import (
	"reflect"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// NodeTypeSchema describes one registered node type for the editor.
type NodeTypeSchema struct {
	Type   string        `json:"type"`
	Fields []FieldSchema `json:"fields"`
}

// FieldSchema describes one config field. Type is one of:
// "string", "number", "bool", "string[]", "raw". Complex/nested fields
// (nested graphs, maps, json.RawMessage, interface{}, struct slices) are "raw":
// the frontend edits them with a raw-JSON textbox.
type FieldSchema struct {
	Name     string `json:"name"`     // Go field name
	JSONName string `json:"jsonName"` // json tag name (without ,omitempty)
	Type     string `json:"type"`
	Optional bool   `json:"optional"` // json tag has ,omitempty
}

// BuildNodeTypeSchemas reflects every registered node type's config prototype
// into a schema, sorted by type name (ListNodeTypes is sorted).
func BuildNodeTypeSchemas() []NodeTypeSchema {
	names := orchestrator.ListNodeTypes()
	out := make([]NodeTypeSchema, 0, len(names))
	for _, name := range names {
		entry, ok := orchestrator.LookupNodeType(name)
		if !ok {
			continue
		}
		out = append(out, NodeTypeSchema{
			Type:   name,
			Fields: fieldsOf(entry.ConfigPrototype),
		})
	}
	return out
}

func fieldsOf(proto interface{}) []FieldSchema {
	fields := []FieldSchema{}
	if proto == nil {
		return fields
	}
	t := reflect.TypeOf(proto)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, optional := parseJSONTag(tag, f.Name)
		fields = append(fields, FieldSchema{
			Name:     f.Name,
			JSONName: name,
			Type:     fieldType(f.Type),
			Optional: optional,
		})
	}
	return fields
}

func parseJSONTag(tag, fieldName string) (name string, optional bool) {
	if tag == "" {
		return fieldName, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fieldName
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			optional = true
		}
	}
	return name, optional
}

func fieldType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "string[]"
		}
		return "raw"
	default:
		return "raw"
	}
}
```

> 注:`json.RawMessage` 是 `[]byte`(elem kind = Uint8,非 String)→ `raw`;`[]ChoiceEntry`(elem = Struct)→ `raw`;`map`/`interface{}` → `raw`。符合预期。

- [ ] **Step 4: 运行测试,确认通过**

Run: `go test ./pkg/grapheditor/ -run TestBuildNodeTypeSchemas -v`
Expected: PASS(两个用例)。

- [ ] **Step 5: 提交**

```bash
git add pkg/grapheditor/schema.go pkg/grapheditor/schema_test.go
git commit -m "feat(grapheditor): reflect node config prototypes into field schemas"
```

---

### Task 3: `grapheditor` 图校验

**Files:**
- Create: `pkg/grapheditor/validate.go`
- Test: `pkg/grapheditor/validate_test.go`

- [ ] **Step 1: 写失败测试**

Create `pkg/grapheditor/validate_test.go`:

```go
package grapheditor

import (
	"strings"
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

const validGraph = `{
  "StartAt": "a",
  "Nodes": {
    "a": {"Type": "end", "Config": {"Status": "success"}},
    "b": {"Type": "end", "Config": {"Status": "success"}}
  },
  "Edges": [{"From": "a", "To": "b"}]
}`

func TestValidateGraph_Valid(t *testing.T) {
	resp := ValidateGraph([]byte(validGraph))
	if !resp.Valid {
		t.Fatalf("expected valid, got errors: %+v", resp.Errors)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("expected no errors, got: %+v", resp.Errors)
	}
}

func TestValidateGraph_BadCondition(t *testing.T) {
	bad := `{
      "StartAt": "a",
      "Nodes": {
        "a": {"Type": "end", "Config": {"Status": "success"}},
        "b": {"Type": "end", "Config": {"Status": "success"}}
      },
      "Edges": [{"From": "a", "To": "b", "Condition": "input.x === 1"}]
    }`
	resp := ValidateGraph([]byte(bad))
	if resp.Valid {
		t.Fatal("expected invalid for bad condition")
	}
	if len(resp.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
	// message should reference the offending edge
	joined := resp.Errors[0].Message
	if !strings.Contains(joined, "edge 0") {
		t.Fatalf("error message should reference edge 0, got: %q", joined)
	}
}

func TestValidateGraph_DanglingEdge(t *testing.T) {
	bad := `{
      "StartAt": "a",
      "Nodes": {"a": {"Type": "end", "Config": {"Status": "success"}}},
      "Edges": [{"From": "a", "To": "ghost"}]
    }`
	resp := ValidateGraph([]byte(bad))
	if resp.Valid {
		t.Fatal("expected invalid for dangling edge")
	}
	found := false
	for _, e := range resp.Errors {
		if e.Path == "edges[0].to" && strings.Contains(e.Message, "ghost") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected edges[0].to error about ghost, got: %+v", resp.Errors)
	}
}

func TestValidateGraph_MissingStart(t *testing.T) {
	bad := `{
      "StartAt": "missing",
      "Nodes": {"a": {"Type": "end", "Config": {"Status": "success"}}},
      "Edges": []
    }`
	resp := ValidateGraph([]byte(bad))
	if resp.Valid {
		t.Fatal("expected invalid for missing start node")
	}
	found := false
	for _, e := range resp.Errors {
		if e.Path == "StartAt" && strings.Contains(e.Message, "missing") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected StartAt error about missing, got: %+v", resp.Errors)
	}
}
```

- [ ] **Step 2: 运行测试,确认失败**

Run: `go test ./pkg/grapheditor/ -run TestValidateGraph -v`
Expected: 编译失败 —— `undefined: ValidateGraph` / `ValidateResponse` / `ValidationError`。

- [ ] **Step 3: 实现 `validate.go`**

Create `pkg/grapheditor/validate.go`:

```go
package grapheditor

import (
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ValidationError is one problem found in a graph, with a best-effort path.
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// ValidateResponse is the result of validating a graph JSON document.
type ValidateResponse struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors"`
}

// ValidateGraph parses and structurally checks a graph JSON document.
// Parse-level failures (structure, unknown node types, bad config, bad edge
// conditions) surface from UnmarshalGraph as a single error with path
// "<graph>". If parsing succeeds, structural checks (StartAt and edge
// endpoints referencing existing nodes) run with precise paths.
func ValidateGraph(body []byte) ValidateResponse {
	g, err := orchestrator.UnmarshalGraph(body)
	if err != nil {
		return ValidateResponse{
			Valid:  false,
			Errors: []ValidationError{{Path: "<graph>", Message: err.Error()}},
		}
	}

	errs := []ValidationError{}

	switch {
	case g.StartAt == "":
		errs = append(errs, ValidationError{Path: "StartAt", Message: "StartAt is empty"})
	default:
		if _, ok := g.Nodes[g.StartAt]; !ok {
			errs = append(errs, ValidationError{
				Path:    "StartAt",
				Message: fmt.Sprintf("StartAt references unknown node %q", g.StartAt),
			})
		}
	}

	for i, e := range g.Edges {
		if e.From != "" {
			if _, ok := g.Nodes[e.From]; !ok {
				errs = append(errs, ValidationError{
					Path:    fmt.Sprintf("edges[%d].from", i),
					Message: fmt.Sprintf("edge %d: From references unknown node %q", i, e.From),
				})
			}
		}
		if e.To != "" {
			if _, ok := g.Nodes[e.To]; !ok {
				errs = append(errs, ValidationError{
					Path:    fmt.Sprintf("edges[%d].to", i),
					Message: fmt.Sprintf("edge %d: To references unknown node %q", i, e.To),
				})
			}
		}
	}

	return ValidateResponse{Valid: len(errs) == 0, Errors: errs}
}
```

- [ ] **Step 4: 运行测试,确认通过**

Run: `go test ./pkg/grapheditor/ -run TestValidateGraph -v`
Expected: PASS(四个用例)。

- [ ] **Step 5: 提交**

```bash
git add pkg/grapheditor/validate.go pkg/grapheditor/validate_test.go
git commit -m "feat(grapheditor): validate graph JSON (parse + structural checks)"
```

---

### Task 4: `grapheditor` HTTP handler + 内嵌静态资源

**Files:**
- Create: `pkg/grapheditor/static/index.html`(占位前端,必须先建,`go:embed` 才能编译)
- Create: `pkg/grapheditor/server.go`
- Test: `pkg/grapheditor/server_test.go`

- [ ] **Step 1: 建占位静态页(embed 目标)**

Create `pkg/grapheditor/static/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Hermes Graph Editor</title></head>
<body>
  <h1>Hermes Graph Editor</h1>
  <p>Editor frontend not built yet. API: <code>GET /api/nodetypes</code>, <code>POST /api/validate</code>.</p>
</body>
</html>
```

- [ ] **Step 2: 写失败测试**

Create `pkg/grapheditor/server_test.go`:

```go
package grapheditor

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func TestHandler_NodeTypes(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/nodetypes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var schemas []NodeTypeSchema
	if err := json.NewDecoder(resp.Body).Decode(&schemas); err != nil {
		t.Fatal(err)
	}
	if findSchema(schemas, "llm") == nil {
		t.Fatalf("expected llm in schemas, got %d types", len(schemas))
	}
}

func TestHandler_ValidateOK(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/validate", "application/json",
		strings.NewReader(validGraph))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var vr ValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		t.Fatal(err)
	}
	if !vr.Valid {
		t.Fatalf("expected valid, got %+v", vr.Errors)
	}
}

func TestHandler_ValidateNonJSON(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/validate", "text/plain",
		strings.NewReader("this is not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandler_ServesIndex(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Hermes Graph Editor") {
		t.Fatalf("index page not served, body: %q", string(body))
	}
}
```

> `findSchema` 与 `validGraph` 已在前面 Task 的同包测试文件中定义,可直接复用。

- [ ] **Step 3: 运行测试,确认失败**

Run: `go test ./pkg/grapheditor/ -run TestHandler -v`
Expected: 编译失败 —— `undefined: NewHandler`。

- [ ] **Step 4: 实现 `server.go`**

Create `pkg/grapheditor/server.go`:

```go
package grapheditor

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// NewHandler builds the editor HTTP handler: the two JSON APIs plus the
// embedded static frontend served at the root.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/nodetypes", handleNodeTypes)
	mux.HandleFunc("/api/validate", handleValidate)

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embedded dir is known at build time
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return mux
}

func handleNodeTypes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, BuildNodeTypeSchemas())
}

func handleValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !json.Valid(body) {
		writeJSON(w, http.StatusBadRequest,
			map[string]string{"error": "request body is not valid JSON"})
		return
	}
	writeJSON(w, http.StatusOK, ValidateGraph(body))
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 5: 运行测试,确认通过**

Run: `go test ./pkg/grapheditor/ -v`
Expected: 本包全部用例 PASS(schema / validate / handler)。

- [ ] **Step 6: 提交**

```bash
git add pkg/grapheditor/server.go pkg/grapheditor/server_test.go pkg/grapheditor/static/index.html
git commit -m "feat(grapheditor): HTTP handler with node-types/validate APIs and embedded static frontend"
```

---

### Task 5: `cmd/hermes-editor` 二进制

**Files:**
- Create: `cmd/hermes-editor/main.go`

- [ ] **Step 1: 实现 main**

Create `cmd/hermes-editor/main.go`:

```go
package main

import (
	"flag"
	"log"
	"net/http"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/grapheditor"
	// Blank import registers all node types (llm/tool/choice/parallel/human/end)
	// via their init() funcs, so ListNodeTypes/schema export sees them.
	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7390", "address to listen on (host:port)")
	flag.Parse()

	log.Printf("hermes graph editor listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, grapheditor.NewHandler()); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
```

- [ ] **Step 2: 构建,确认通过**

Run: `go build ./cmd/hermes-editor/`
Expected: 编译成功,生成 `hermes-editor`(Windows 为 `hermes-editor.exe`)。

- [ ] **Step 3: 手动冒烟(启动 + 驱动)**

启动(后台)并打三个请求验证端到端:

```bash
go run ./cmd/hermes-editor/ -addr 127.0.0.1:7391 &
sleep 2
curl -s http://127.0.0.1:7391/api/nodetypes | head -c 300; echo
curl -s -X POST http://127.0.0.1:7391/api/validate \
  -H 'Content-Type: application/json' \
  -d '{"StartAt":"a","Nodes":{"a":{"Type":"end","Config":{"Status":"success"}}},"Edges":[]}'; echo
curl -s http://127.0.0.1:7391/ | head -c 200; echo
```

Expected:
- `/api/nodetypes` → 含 `{"type":"llm",...}` 等的 JSON 数组。
- `/api/validate` → `{"valid":true,"errors":[]}`。
- `/` → 含 `Hermes Graph Editor` 的 HTML。

(Windows PowerShell 等价:`Start-Process`/`Invoke-WebRequest`;或在 git-bash 下用上面的命令。)关闭后台进程后继续。

- [ ] **Step 4: 提交**

```bash
git add cmd/hermes-editor/main.go
git commit -m "feat(cmd): add hermes-editor dev server binary"
```

---

### Task 6: 全量校验

**Files:** 无(校验任务)

- [ ] **Step 1: 构建 + vet + 格式 + 测试**

```bash
go build ./...
go vet ./...
gofmt -l pkg/grapheditor cmd/hermes-editor pkg/orchestrator/registry.go
go test ./...
```

Expected:
- `go build ./...`、`go vet ./...` 无输出(成功)。
- `gofmt -l` 无输出(已格式化;若有,运行 `gofmt -w <文件>` 修正后重测)。
- `go test ./...` 全绿。

> `-race` 本机无 CGO/gcc 跑不了,沿用既有约束;本子项目无并发逻辑,不强制。

- [ ] **Step 2: 提交(若 gofmt 有修正)**

```bash
git add -A
git commit -m "style: gofmt graph editor backend"
```

(无改动则跳过。)

---

## Self-Review

**1. Spec coverage:**
- 反射 schema 导出 → Task 2 ✅
- 复杂类型降级 raw → Task 2 `fieldType`/测试 OutputSchema/Choices/Parameters ✅
- `ListNodeTypes` → Task 1 ✅
- 校验 API(parse + 结构 + 条件)→ Task 3(条件经 UnmarshalGraph,结构在 ValidateGraph)✅
- 校验失败 200 + valid:false;非 JSON 400 → Task 4 handler ✅
- 独立二进制 + blank import runner + 127.0.0.1 默认端口 → Task 5 ✅
- 内嵌占位静态页 → Task 4 ✅
- 不落盘 → 全程无文件读写 ✅
- 全量 build/vet/gofmt/test → Task 6 ✅

**2. Placeholder scan:** 无 TBD/TODO;每个代码步骤含完整代码。✅

**3. Type consistency:** `NodeTypeSchema`/`FieldSchema`/`ValidationError`/`ValidateResponse`/`BuildNodeTypeSchemas`/`ValidateGraph`/`NewHandler` 在定义与测试/调用处命名一致;`findSchema`/`findField`/`validGraph` 在同包测试中单处定义、跨文件复用(Task 2 定义,Task 3/4 复用)。✅

**与 spec 的已知偏差(已在抬头说明):** 无 `Graph.Validate()` 方法,结构校验内置于 `grapheditor`;parse 层错误 path 为 `<graph>`,结构层 path 精确。
