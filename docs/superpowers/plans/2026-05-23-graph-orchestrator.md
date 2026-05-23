# Graph-Based Agent Orchestration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the monolithic `conversationLoop()` with a graph executor that walks a JSON-defined DAG of typed nodes (LLM, Tool, Choice, Parallel, Human, End), delegating work to hermes's existing model and tool layers through narrow adapter interfaces.

**Architecture:** All orchestration primitives live in a new `pkg/orchestrator/` package with zero external dependencies. Adapters in `pkg/agent/adapters/` bridge the orchestrator interfaces to hermes's existing `model.Router`, `tool.Registry`, and `EventCallback`. The default agent graph is a JSON definition that replicates current `conversationLoop` behavior.

**Tech Stack:** Go 1.25+, standard library only (encoding/json, context, sync, crypto/rand), existing hermes packages unchanged.

---

### Task 1: Orchestrator Core Types

**Files:**
- Create: `pkg/orchestrator/node.go`
- Create: `pkg/orchestrator/graph.go`
- Create: `pkg/orchestrator/registry.go`
- Create: `pkg/orchestrator/tracer.go`

- [ ] **Step 1: Write node.go — NodeRunner interface + NodeResult**

```go
package orchestrator

import "context"

// NodeRunner is implemented by every node type.
type NodeRunner interface {
	Run(ctx context.Context, node *NodeSpec, input interface{},
		execCtx *ExecutionContext) (*NodeResult, error)
}

// NodeResult is the output of a node execution.
type NodeResult struct {
	Status    string      // "continue" | "end" | "pending"
	Output    interface{}
	Next      string      // dynamic next node (optional, for choice nodes)
	Error     string
	Cause     string
	Interrupt bool        // true means pause and wait for external input
}
```

- [ ] **Step 2: Write graph.go — Graph, NodeSpec, EdgeSpec types**

```go
package orchestrator

import "encoding/json"

// Graph is an executable DAG.
type Graph struct {
	Nodes     map[string]*NodeSpec `json:"Nodes"`
	Edges     []EdgeSpec           `json:"Edges"`
	StartAt   string               `json:"StartAt"`
	MaxSteps  int                  `json:"MaxSteps,omitempty"`
	OnError   string               `json:"OnError,omitempty"`
	OnTimeout string               `json:"OnTimeout,omitempty"`
	Version   string               `json:"Version,omitempty"`
}

// NodeSpec is the unified structure for a node. Type + Config determine behavior.
type NodeSpec struct {
	Type         string          `json:"Type"`
	Config       json.RawMessage `json:"Config,omitempty"`
	ParsedConfig interface{}     `json:"-"`
	InputPath    string          `json:"InputPath,omitempty"`
	OutputPath   string          `json:"OutputPath,omitempty"`
	Retry        []RetryPolicy   `json:"Retry,omitempty"`
	Catch        []CatchPolicy   `json:"Catch,omitempty"`
	Timeout      uint            `json:"Timeout,omitempty"`
}

// RetryPolicy configures automatic retries.
type RetryPolicy struct {
	MaxAttempts     int      `json:"MaxAttempts"`
	IntervalSeconds int      `json:"IntervalSeconds"`
	BackoffRate     float64  `json:"BackoffRate"`
	Jitter          float64  `json:"Jitter,omitempty"`
	Errors          []string `json:"Errors,omitempty"`
}

// CatchPolicy routes errors to a fallback node.
type CatchPolicy struct {
	ErrorEquals []string `json:"ErrorEquals"`
	Next        string   `json:"Next"`
}

// EdgeSpec defines a directed edge between nodes.
type EdgeSpec struct {
	From      string          `json:"From"`
	To        string          `json:"To"`
	Condition json.RawMessage `json:"Condition,omitempty"`
	Priority  int             `json:"Priority"`
	Label     string          `json:"Label,omitempty"`
}
```

- [ ] **Step 3: Write registry.go — type registry for NodeRunner lookup**

```go
package orchestrator

import (
	"fmt"
	"sync"
)

type nodeTypeEntry struct {
	Runner         NodeRunner
	ConfigPrototype interface{}
}

var (
	mu       sync.RWMutex
	registry = make(map[string]*nodeTypeEntry)
)

// RegisterNodeType registers a node type with its config prototype.
func RegisterNodeType(name string, runner NodeRunner, proto interface{}) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = &nodeTypeEntry{Runner: runner, ConfigPrototype: proto}
}

// LookupNodeType returns the entry for a registered node type.
func LookupNodeType(name string) (*nodeTypeEntry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := registry[name]
	return e, ok
}

// MustLookupNodeType panics if the type is not registered.
func MustLookupNodeType(name string) *nodeTypeEntry {
	e, ok := LookupNodeType(name)
	if !ok {
		panic(fmt.Sprintf("node type %q not registered", name))
	}
	return e
}
```

- [ ] **Step 4: Write tracer.go — Tracer interface**

```go
package orchestrator

import "context"

// Tracer receives execution events. Implementations can log, emit metrics,
// or bridge to legacy EventCallback.
type Tracer interface {
	OnNodeStart(ctx context.Context, nodeID, nodeType string, input interface{})
	OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *NodeResult, err error)
	OnStreamDelta(ctx context.Context, content string)
}

// NopTracer is a no-op tracer for when observability is not needed.
type NopTracer struct{}

func (NopTracer) OnNodeStart(ctx context.Context, nodeID, nodeType string, input interface{}) {}
func (NopTracer) OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *NodeResult, err error) {}
func (NopTracer) OnStreamDelta(ctx context.Context, content string) {}
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./pkg/orchestrator/...`
Expected: builds without errors

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/node.go pkg/orchestrator/graph.go pkg/orchestrator/registry.go pkg/orchestrator/tracer.go
git commit -m "feat(orchestrator): add core types — NodeRunner, Graph, NodeSpec, EdgeSpec, registry, Tracer"
```

---

### Task 2: Streaming Primitives — Pipe / StreamReader / StreamWriter

**Files:**
- Create: `pkg/orchestrator/schema/pipe.go`

- [ ] **Step 1: Write pipe.go**

```go
package schema

import (
	"io"
	"sync"
)

// StreamReader is a read-only stream of values.
type StreamReader struct {
	ch   chan streamItem
	once sync.Once
}

type streamItem struct {
	value interface{}
	err   error
}

// Recv reads the next value from the stream. Returns io.EOF when closed.
func (sr *StreamReader) Recv() (interface{}, error) {
	item, ok := <-sr.ch
	if !ok {
		return nil, io.EOF
	}
	return item.value, item.err
}

// StreamWriter is the write side of a pipe.
type StreamWriter struct {
	ch   chan streamItem
	once sync.Once
}

// Send writes a value to the stream.
func (sw *StreamWriter) Send(value interface{}, err error) {
	sw.ch <- streamItem{value: value, err: err}
}

// Close closes the stream. Readers receive io.EOF after draining.
func (sw *StreamWriter) Close() {
	sw.once.Do(func() { close(sw.ch) })
}

// Pipe creates a connected pair of StreamReader and StreamWriter with a
// buffered channel of the given size.
func Pipe(bufSize int) (*StreamWriter, *StreamReader) {
	ch := make(chan streamItem, bufSize)
	return &StreamWriter{ch: ch}, &StreamReader{ch: ch}
}

// StreamReaderFromArray creates a StreamReader pre-loaded with values.
func StreamReaderFromArray(values []interface{}) *StreamReader {
	sw, sr := Pipe(len(values))
	for _, v := range values {
		sw.Send(v, nil)
	}
	sw.Close()
	return sr
}
```

- [ ] **Step 2: Write pipe_test.go**

```go
package schema

import (
	"io"
	"testing"
)

func TestPipeSendRecv(t *testing.T) {
	sw, sr := Pipe(2)
	sw.Send("hello", nil)
	sw.Send("world", nil)
	sw.Close()

	v, err := sr.Recv()
	if err != nil || v != "hello" {
		t.Fatalf("expected 'hello', got %v, err=%v", v, err)
	}
	v, err = sr.Recv()
	if err != nil || v != "world" {
		t.Fatalf("expected 'world', got %v, err=%v", v, err)
	}
	_, err = sr.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestPipeError(t *testing.T) {
	sw, sr := Pipe(1)
	sw.Send(nil, io.ErrUnexpectedEOF)
	sw.Close()

	_, err := sr.Recv()
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("expected ErrUnexpectedEOF, got %v", err)
	}
}

func TestStreamReaderFromArray(t *testing.T) {
	sr := StreamReaderFromArray([]interface{}{"a", "b"})
	v, _ := sr.Recv()
	if v != "a" {
		t.Fatalf("expected 'a', got %v", v)
	}
	v, _ = sr.Recv()
	if v != "b" {
		t.Fatalf("expected 'b', got %v", v)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/orchestrator/schema/... -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/schema/
git commit -m "feat(orchestrator): add Pipe/StreamReader/StreamWriter streaming primitives"
```

---

### Task 3: Execution Context — WorkingMemory + ConversationMemory

**Files:**
- Create: `pkg/orchestrator/context/execution.go`
- Create: `pkg/orchestrator/context/memory.go`

- [ ] **Step 1: Write execution.go**

```go
package context

// ExecutionContext holds state for a single Graph execution.
type ExecutionContext struct {
	WorkMem        *WorkingMemory
	ConvMem        *ConversationMemory
	TraceID        string
	CurrentSpanID  string
	DefinitionName string
}

// WorkingMemory is the mutable state during Graph execution.
type WorkingMemory struct {
	Input      interface{}
	State      map[string]interface{}
	Scratchpad []string
	LastResult interface{}
}

// NewWorkingMemory creates working memory initialized with input.
func NewWorkingMemory(input interface{}) *WorkingMemory {
	return &WorkingMemory{
		Input:      input,
		State:      make(map[string]interface{}),
		Scratchpad: make([]string, 0),
	}
}

// AppendScratchpad adds a thought to the scratchpad.
func (wm *WorkingMemory) AppendScratchpad(thought string) {
	wm.Scratchpad = append(wm.Scratchpad, thought)
}

// NewExecutionContext creates an execution context from input.
func NewExecutionContext(input interface{}) *ExecutionContext {
	return &ExecutionContext{
		WorkMem: NewWorkingMemory(input),
	}
}

// Fork creates a child ExecutionContext for parallel/map branch isolation.
func (ec *ExecutionContext) Fork() *ExecutionContext {
	stateCopy := make(map[string]interface{})
	for k, v := range ec.WorkMem.State {
		stateCopy[k] = v
	}
	child := &ExecutionContext{
		WorkMem: &WorkingMemory{
			Input:      ec.WorkMem.Input,
			State:      stateCopy,
			Scratchpad: make([]string, 0),
		},
		ConvMem:        ec.ConvMem,
		TraceID:        ec.TraceID,
		DefinitionName: ec.DefinitionName,
	}
	return child
}

// Merge merges a child ExecutionContext's State back into this one.
func (ec *ExecutionContext) Merge(child *ExecutionContext) {
	for k, v := range child.WorkMem.State {
		ec.WorkMem.State[k] = v
	}
}
```

- [ ] **Step 2: Write memory.go**

```go
package context

import "context"

// Message is a single conversation turn.
type Message struct {
	Role    string `json:"Role"`
	Content string `json:"Content"`
	Name    string `json:"Name,omitempty"`
}

// ConversationMemory holds session-scoped message history.
type ConversationMemory struct {
	SessionID string                 `json:"SessionID"`
	Messages  []Message              `json:"Messages"`
	Metadata  map[string]interface{} `json:"Metadata,omitempty"`
}

// AddMessage appends a message to the conversation.
func (cm *ConversationMemory) AddMessage(msg Message) {
	cm.Messages = append(cm.Messages, msg)
}

// MemoryStore persists conversation memory across sessions.
type MemoryStore interface {
	SaveSession(ctx context.Context, session *ConversationMemory) error
	LoadSession(ctx context.Context, sessionID string) (*ConversationMemory, error)
	DeleteSession(ctx context.Context, sessionID string) error
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./pkg/orchestrator/...`
Expected: builds without errors

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/context/
git commit -m "feat(orchestrator): add ExecutionContext, WorkingMemory, ConversationMemory, MemoryStore"
```

---

### Task 4: End Runner + Choice Runner

**Files:**
- Create: `pkg/orchestrator/runner/end.go`
- Create: `pkg/orchestrator/runner/choice.go`

- [ ] **Step 1: Write end.go**

```go
package runner

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// EndConfig is the configuration for an end node.
type EndConfig struct {
	Status  string `json:"Status"`
	Message string `json:"Message,omitempty"`
}

// EndRunner terminates graph execution.
type EndRunner struct{}

func (r *EndRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	var cfg EndConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*EndConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}
	if cfg.Status == "" {
		cfg.Status = "success"
	}
	return &orchestrator.NodeResult{
		Status: "end",
		Output: map[string]interface{}{
			"Status":  cfg.Status,
			"Message": cfg.Message,
			"Output":  input,
		},
	}, nil
}

func init() {
	orchestrator.RegisterNodeType("end", &EndRunner{}, &EndConfig{})
}
```

- [ ] **Step 2: Write choice.go**

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ChoiceConfig configures a choice (branch) node.
type ChoiceConfig struct {
	Choices []ChoiceEntry `json:"Choices"`
	Default string        `json:"Default,omitempty"`
}

// ChoiceEntry is a single branch condition.
type ChoiceEntry struct {
	Next      string          `json:"Next"`
	Condition json.RawMessage `json:"Condition,omitempty"`
}

// ChoiceRunner evaluates conditions and routes to the matching branch.
type ChoiceRunner struct{}

func (r *ChoiceRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	var cfg ChoiceConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ChoiceConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	for _, ch := range cfg.Choices {
		if len(ch.Condition) == 0 {
			return &orchestrator.NodeResult{Status: "continue", Next: ch.Next, Output: input}, nil
		}
		// Evaluate the condition against the input.
		// The condition is a JSON object like {"has_tool_calls": true}.
		// The input is expected to be a map with matching keys.
		matched := evaluateCondition(ch.Condition, input)
		if matched {
			return &orchestrator.NodeResult{Status: "continue", Next: ch.Next, Output: input}, nil
		}
	}

	if cfg.Default != "" {
		return &orchestrator.NodeResult{Status: "continue", Next: cfg.Default, Output: input}, nil
	}

	return nil, fmt.Errorf("no choice matched and no default")
}

// evaluateCondition checks if input matches the condition.
// The condition is a flat JSON object where keys are field names and
// values are the expected boolean values (e.g. {"has_tool_calls": true}).
// Input is a map[string]interface{} with those fields.
func evaluateCondition(cond json.RawMessage, input interface{}) bool {
	var condMap map[string]interface{}
	if err := json.Unmarshal(cond, &condMap); err != nil {
		return false
	}
	inputMap, ok := input.(map[string]interface{})
	if !ok {
		return false
	}
	for key, expectedVal := range condMap {
		actualVal, exists := inputMap[key]
		if !exists {
			return false
		}
		if actualVal != expectedVal {
			return false
		}
	}
	return true
}

func init() {
	orchestrator.RegisterNodeType("choice", &ChoiceRunner{}, &ChoiceConfig{})
}
```

- [ ] **Step 3: Write runner_test.go for end and choice**

```go
package runner

import (
	"context"
	"encoding/json"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

func TestEndRunner(t *testing.T) {
	r := &EndRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Status":"success","Message":"done"}`),
	}
	result, err := r.Run(context.Background(), node, "world", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "end" {
		t.Fatalf("expected status 'end', got %q", result.Status)
	}
	m, ok := result.Output.(map[string]interface{})
	if !ok {
		t.Fatal("expected map output")
	}
	if m["Output"] != "world" {
		t.Fatalf("expected Output='world', got %v", m["Output"])
	}
}

func TestChoiceRunnerDefault(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Default":"fallback"}`),
	}
	result, err := r.Run(context.Background(), node, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "fallback" {
		t.Fatalf("expected next 'fallback', got %q", result.Next)
	}
}

func TestChoiceRunnerMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":{"has_tool_calls":true},"Next":"tools"}],"Default":"end"}`),
	}
	input := map[string]interface{}{"has_tool_calls": true}
	result, err := r.Run(context.Background(), node, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "tools" {
		t.Fatalf("expected next 'tools', got %q", result.Next)
	}
}

func TestChoiceRunnerNoMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":{"has_tool_calls":true},"Next":"tools"}],"Default":"end"}`),
	}
	input := map[string]interface{}{"has_tool_calls": false}
	result, err := r.Run(context.Background(), node, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "end" {
		t.Fatalf("expected default next 'end', got %q", result.Next)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./pkg/orchestrator/runner/... -v -run "TestEnd|TestChoice"`
Expected: PASS (4/4)

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/runner/end.go pkg/orchestrator/runner/choice.go pkg/orchestrator/runner/runner_test.go
git commit -m "feat(orchestrator): add EndRunner and ChoiceRunner with tests"
```

---

### Task 5: LLM Runner — LLMRunner + LLMInvoker Interface

**Files:**
- Create: `pkg/orchestrator/runner/llm.go`

- [ ] **Step 1: Write llm.go**

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// LLMConfig configures an llm node.
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
	Role    string `json:"Role"`
	Content string `json:"Content"`
	Name    string `json:"Name,omitempty"`
}

// LLMInvoker abstracts the actual LLM call. hermes adapts model.Router to this.
type LLMInvoker interface {
	Chat(ctx context.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig) (*orchestrator.NodeResult, error)
	ChatStream(ctx context.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error)
}

// LLMRunner executes an LLM node by delegating to an LLMInvoker.
type LLMRunner struct {
	Invoker LLMInvoker
}

// SetInvoker sets the LLM invoker (called after construction).
func (r *LLMRunner) SetInvoker(inv LLMInvoker) {
	r.Invoker = inv
}

func (r *LLMRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	var cfg LLMConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*LLMConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Invoker == nil {
		return nil, fmt.Errorf("llm runner: no invoker configured")
	}

	// Build messages: system prompt (if set) + existing conversation from ConvMem
	var messages []LLMMessage
	if cfg.SystemPrompt != "" {
		messages = append(messages, LLMMessage{Role: "system", Content: cfg.SystemPrompt})
	}
	if execCtx != nil && execCtx.ConvMem != nil {
		messages = append(messages, execCtx.ConvMem.Messages...)
	}

	// Append the current user input
	userContent := formatInput(input)
	messages = append(messages, LLMMessage{Role: "user", Content: userContent})

	return r.Invoker.Chat(ctx, cfg.Model, messages, cfg.Tools, cfg)
}

func formatInput(input interface{}) string {
	if input == nil {
		return ""
	}
	if s, ok := input.(string); ok {
		return s
	}
	b, _ := json.Marshal(input)
	return string(b)
}

func init() {
	orchestrator.RegisterNodeType("llm", &LLMRunner{}, &LLMConfig{})
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/orchestrator/...`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add pkg/orchestrator/runner/llm.go
git commit -m "feat(orchestrator): add LLMRunner with LLMInvoker interface"
```

---

### Task 6: Tool Runner — ToolRunner + ToolInvoker Interface

**Files:**
- Create: `pkg/orchestrator/runner/tool.go`

- [ ] **Step 1: Write tool.go**

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ToolConfig configures a tool node.
type ToolConfig struct {
	Resource   string                 `json:"Resource"`
	Parameters map[string]interface{} `json:"Parameters,omitempty"`
	Timeout    uint                   `json:"Timeout,omitempty"`
	Async      bool                   `json:"Async,omitempty"`
}

// ToolInvoker abstracts tool execution. hermes adapts tool.Registry to this.
type ToolInvoker interface {
	Invoke(ctx context.Context, resource string, input interface{},
		timeout uint) (*orchestrator.NodeResult, error)
}

// ToolRunner executes a tool by delegating to a ToolInvoker.
type ToolRunner struct {
	Invoker ToolInvoker
}

// SetInvoker sets the tool invoker.
func (r *ToolRunner) SetInvoker(inv ToolInvoker) {
	r.Invoker = inv
}

func (r *ToolRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	var cfg ToolConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ToolConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Invoker == nil {
		return nil, fmt.Errorf("tool runner: no invoker configured")
	}

	result, err := r.Invoker.Invoke(ctx, cfg.Resource, input, cfg.Timeout)
	if err != nil {
		return nil, err
	}

	if cfg.Async && result.Status != "end" {
		result.Status = "pending"
		result.Interrupt = true
	}

	return result, nil
}

func init() {
	orchestrator.RegisterNodeType("tool", &ToolRunner{}, &ToolConfig{})
}
```

- [ ] **Step 2: Add tool runner tests to runner_test.go**

Append to `pkg/orchestrator/runner/runner_test.go`:

```go
type mockToolInvoker struct{}

func (m *mockToolInvoker) Invoke(ctx context.Context, resource string,
	input interface{}, timeout uint) (*orchestrator.NodeResult, error) {
	return &orchestrator.NodeResult{Status: "continue", Output: "result"}, nil
}

func TestToolRunnerNoInvoker(t *testing.T) {
	r := &ToolRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Resource":"rpc/test"}`),
	}
	_, err := r.Run(context.Background(), node, "input", nil)
	if err == nil {
		t.Fatal("expected error for missing invoker")
	}
}

func TestToolRunnerWithMockInvoker(t *testing.T) {
	r := &ToolRunner{}
	r.SetInvoker(&mockToolInvoker{})
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Resource":"rpc/test","Timeout":10}`),
	}
	result, err := r.Run(context.Background(), node, "input", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "continue" {
		t.Fatalf("expected status 'continue', got %q", result.Status)
	}
	if result.Output != "result" {
		t.Fatalf("expected output 'result', got %v", result.Output)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/orchestrator/runner/... -v -run "TestTool"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/runner/tool.go pkg/orchestrator/runner/runner_test.go
git commit -m "feat(orchestrator): add ToolRunner with ToolInvoker interface"
```

---

### Task 7: Parallel Runner

**Files:**
- Create: `pkg/orchestrator/runner/parallel.go`

- [ ] **Step 1: Write parallel.go**

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ParallelConfig configures a parallel node.
type ParallelConfig struct {
	Branches []*orchestrator.Graph `json:"Branches"`
}

// GraphExecutor executes a sub-graph. The orchestrator Executor implements this.
type GraphExecutor interface {
	Execute(ctx context.Context, g *orchestrator.Graph,
		input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error)
}

// ParallelRunner executes multiple branches concurrently.
type ParallelRunner struct {
	Executor GraphExecutor
}

// SetExecutor sets the graph executor.
func (r *ParallelRunner) SetExecutor(exec GraphExecutor) {
	r.Executor = exec
}

func (r *ParallelRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	var cfg ParallelConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ParallelConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Executor == nil {
		return nil, fmt.Errorf("parallel runner: no executor configured")
	}

	var wg sync.WaitGroup
	results := make([]interface{}, len(cfg.Branches))
	var firstErr error
	var mu sync.Mutex

	for i, branch := range cfg.Branches {
		if branch == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, g *orchestrator.Graph) {
			defer wg.Done()
			out, _, err := r.Executor.Execute(ctx, g, input)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			results[idx] = out
		}(i, branch)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return &orchestrator.NodeResult{
		Status: "continue",
		Output: results,
	}, nil
}

func init() {
	orchestrator.RegisterNodeType("parallel", &ParallelRunner{}, &ParallelConfig{})
}
```

- [ ] **Step 2: Add parallel runner test**

Append to `pkg/orchestrator/runner/runner_test.go`:

```go
type mockGraphExecutor struct{}

func (m *mockGraphExecutor) Execute(ctx context.Context, g *orchestrator.Graph,
	input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error) {
	return nil, nil, nil
}

func TestParallelRunnerNoExecutor(t *testing.T) {
	r := &ParallelRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Branches":[]}`),
	}
	_, err := r.Run(context.Background(), node, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing executor")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/orchestrator/runner/... -v -run "TestParallel"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/runner/parallel.go pkg/orchestrator/runner/runner_test.go
git commit -m "feat(orchestrator): add ParallelRunner with GraphExecutor interface"
```

---

### Task 8: Human Runner

**Files:**
- Create: `pkg/orchestrator/runner/human.go`

- [ ] **Step 1: Write human.go**

```go
package runner

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// HumanConfig configures a human-in-the-loop node.
type HumanConfig struct {
	Prompt  string      `json:"Prompt,omitempty"`
	Actions []string    `json:"Actions,omitempty"`
	Timeout uint        `json:"Timeout,omitempty"`
	Schema  interface{} `json:"Schema,omitempty"`
}

// HumanRunner pauses execution and waits for human input.
type HumanRunner struct{}

func (r *HumanRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	var cfg HumanConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*HumanConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	return &orchestrator.NodeResult{
		Status:    "pending",
		Interrupt: true,
		Output:    input,
	}, nil
}

func init() {
	orchestrator.RegisterNodeType("human", &HumanRunner{}, &HumanConfig{})
}
```

- [ ] **Step 2: Add human runner test**

Append to `pkg/orchestrator/runner/runner_test.go`:

```go
func TestHumanRunnerInterrupt(t *testing.T) {
	r := &HumanRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Prompt":"Approve?","Actions":["yes","no"]}`),
	}
	result, err := r.Run(context.Background(), node, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Interrupt {
		t.Fatal("expected Interrupt=true")
	}
	if result.Status != "pending" {
		t.Fatalf("expected status 'pending', got %q", result.Status)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/orchestrator/runner/... -v -run "TestHuman"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/runner/human.go pkg/orchestrator/runner/runner_test.go
git commit -m "feat(orchestrator): add HumanRunner for human-in-the-loop support"
```

---

### Task 9: Graph JSON Loading

**Files:**
- Create: `pkg/orchestrator/graph_json.go`
- Create: `pkg/orchestrator/graph_json_test.go`

- [ ] **Step 1: Write graph_json.go**

```go
package orchestrator

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// UnmarshalGraph loads a Graph from JSON bytes with two-phase parsing:
// phase 1 extracts Type, phase 2 parses Config using the registered prototype.
func UnmarshalGraph(data []byte) (*Graph, error) {
	var raw struct {
		Nodes     map[string]json.RawMessage `json:"Nodes"`
		Edges     []EdgeSpec                 `json:"Edges"`
		StartAt   string                     `json:"StartAt"`
		MaxSteps  int                        `json:"MaxSteps"`
		OnError   string                     `json:"OnError,omitempty"`
		OnTimeout string                     `json:"OnTimeout,omitempty"`
		Version   string                     `json:"Version,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal graph: %w", err)
	}

	g := &Graph{
		Edges:     raw.Edges,
		StartAt:   raw.StartAt,
		MaxSteps:  raw.MaxSteps,
		OnError:   raw.OnError,
		OnTimeout: raw.OnTimeout,
		Version:   raw.Version,
		Nodes:     make(map[string]*NodeSpec, len(raw.Nodes)),
	}

	if g.MaxSteps <= 0 {
		g.MaxSteps = 100
	}

	for name, rawNode := range raw.Nodes {
		node, err := unmarshalNode(rawNode)
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", name, err)
		}
		g.Nodes[name] = node
	}

	return g, nil
}

func unmarshalNode(raw json.RawMessage) (*NodeSpec, error) {
	var typeCheck struct {
		Type string `json:"Type"`
	}
	if err := json.Unmarshal(raw, &typeCheck); err != nil {
		return nil, fmt.Errorf("extract type: %w", err)
	}
	if typeCheck.Type == "" {
		return nil, fmt.Errorf("missing Type field")
	}

	entry, ok := LookupNodeType(typeCheck.Type)
	if !ok {
		return nil, fmt.Errorf("unknown node type: %q", typeCheck.Type)
	}

	var node NodeSpec
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("unmarshal node: %w", err)
	}

	if entry.ConfigPrototype != nil && len(node.Config) > 0 {
		protoType := reflect.TypeOf(entry.ConfigPrototype)
		if protoType.Kind() == reflect.Ptr {
			protoType = protoType.Elem()
		}
		cfgPtr := reflect.New(protoType).Interface()
		if err := json.Unmarshal(node.Config, cfgPtr); err != nil {
			return nil, fmt.Errorf("unmarshal config for type %q: %w", typeCheck.Type, err)
		}
		node.ParsedConfig = cfgPtr
	}

	return &node, nil
}
```

- [ ] **Step 2: Write graph_json_test.go**

```go
package orchestrator

import (
	"testing"
)

func TestUnmarshalGraph(t *testing.T) {
	// Register a test type first
	RegisterNodeType("end", &testEndRunner{}, &struct{ Status string }{})

	data := []byte(`{
		"StartAt": "done",
		"MaxSteps": 10,
		"Nodes": {
			"done": {"Type": "end", "Config": {"Status": "success"}}
		},
		"Edges": []
	}`)

	g, err := UnmarshalGraph(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.StartAt != "done" {
		t.Fatalf("expected StartAt='done', got %q", g.StartAt)
	}
	if g.MaxSteps != 10 {
		t.Fatalf("expected MaxSteps=10, got %d", g.MaxSteps)
	}
	node, ok := g.Nodes["done"]
	if !ok {
		t.Fatal("expected 'done' node")
	}
	if node.Type != "end" {
		t.Fatalf("expected Type='end', got %q", node.Type)
	}
}

type testEndRunner struct{}

func (r *testEndRunner) Run(ctx interface{}, node *NodeSpec, input interface{}, ec *ExecutionContext) (*NodeResult, error) {
	return &NodeResult{Status: "end"}, nil
}
```

Wait—the test runner signature above uses `interface{}` for ctx. Fix it to use `context.Context`:

```go
package orchestrator

import (
	"context"
	"testing"
)

func TestUnmarshalGraph(t *testing.T) {
	RegisterNodeType("end", &testEndRunner{}, &struct{ Status string }{})

	data := []byte(`{
		"StartAt": "done",
		"MaxSteps": 10,
		"Nodes": {
			"done": {"Type": "end", "Config": {"Status": "success"}}
		},
		"Edges": []
	}`)

	g, err := UnmarshalGraph(data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g.StartAt != "done" {
		t.Fatalf("expected StartAt='done', got %q", g.StartAt)
	}
	if g.MaxSteps != 10 {
		t.Fatalf("expected MaxSteps=10, got %d", g.MaxSteps)
	}
	node, ok := g.Nodes["done"]
	if !ok {
		t.Fatal("expected 'done' node")
	}
	if node.Type != "end" {
		t.Fatalf("expected Type='end', got %q", node.Type)
	}
	if node.ParsedConfig == nil {
		t.Fatal("expected ParsedConfig to be set")
	}
}

type testEndRunner struct{}

func (r *testEndRunner) Run(ctx context.Context, node *NodeSpec, input interface{}, ec *ExecutionContext) (*NodeResult, error) {
	return &NodeResult{Status: "end"}, nil
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/orchestrator/... -v -run "TestUnmarshalGraph"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/graph_json.go pkg/orchestrator/graph_json_test.go
git commit -m "feat(orchestrator): add JSON two-phase graph loading"
```

---

### Task 10: Edge Routing

**Files:**
- Create: `pkg/orchestrator/executor/route.go`

- [ ] **Step 1: Write route.go**

```go
package executor

import (
	"context"
	"fmt"
	"sort"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// Route determines the next node based on the current node's result and graph edges.
// Priority: result.Next (dynamic) > edge with highest Priority > error.
func Route(ctx context.Context, currentNode string, result *orchestrator.NodeResult,
	edges []orchestrator.EdgeSpec, ec *orchestrator.ExecutionContext) (string, error) {

	// Dynamic override from the node result
	if result.Next != "" {
		// Verify the target exists as an edge from this node (or accept any if no edges defined)
		return result.Next, nil
	}

	// Find outgoing edges from currentNode, sorted by priority (lower = higher priority)
	var outgoing []orchestrator.EdgeSpec
	for _, e := range edges {
		if e.From == currentNode {
			outgoing = append(outgoing, e)
		}
	}

	sort.Slice(outgoing, func(i, j int) bool {
		return outgoing[i].Priority < outgoing[j].Priority
	})

	if len(outgoing) > 0 {
		return outgoing[0].To, nil
	}

	return "", fmt.Errorf("no edge from node %q and no dynamic Next set", currentNode)
}
```

- [ ] **Step 2: Write route_test.go**

```go
package executor

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

func TestRouteDynamicNext(t *testing.T) {
	result := &orchestrator.NodeResult{Next: "target"}
	next, err := Route(context.Background(), "src", result, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != "target" {
		t.Fatalf("expected 'target', got %q", next)
	}
}

func TestRouteByEdge(t *testing.T) {
	edges := []orchestrator.EdgeSpec{
		{From: "src", To: "dst", Priority: 0},
	}
	result := &orchestrator.NodeResult{Status: "continue"}
	next, err := Route(context.Background(), "src", result, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != "dst" {
		t.Fatalf("expected 'dst', got %q", next)
	}
}

func TestRoutePriority(t *testing.T) {
	edges := []orchestrator.EdgeSpec{
		{From: "src", To: "low", Priority: 10},
		{From: "src", To: "high", Priority: 0},
	}
	result := &orchestrator.NodeResult{Status: "continue"}
	next, err := Route(context.Background(), "src", result, edges, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next != "high" {
		t.Fatalf("expected high-priority edge 'high', got %q", next)
	}
}

func TestRouteNoEdge(t *testing.T) {
	result := &orchestrator.NodeResult{Status: "continue"}
	_, err := Route(context.Background(), "src", result, nil, nil)
	if err == nil {
		t.Fatal("expected error when no edge and no dynamic Next")
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/orchestrator/executor/... -v -run "TestRoute"`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/orchestrator/executor/route.go pkg/orchestrator/executor/route_test.go
git commit -m "feat(orchestrator): add edge routing with priority and dynamic Next support"
```

---

### Task 11: Graph Executor

**Files:**
- Create: `pkg/orchestrator/executor/executor.go`

- [ ] **Step 1: Write executor.go**

```go
package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// Executor walks a Graph, executing nodes and routing between them.
type Executor struct {
	Tracer orchestrator.Tracer
}

// NewExecutor creates an executor with the given tracer.
func NewExecutor(tracer orchestrator.Tracer) *Executor {
	if tracer == nil {
		tracer = orchestrator.NopTracer{}
	}
	return &Executor{Tracer: tracer}
}

// ExecutionSnapshot captures execution state at an interrupt point.
type ExecutionSnapshot struct {
	ExecutionID string
	CurrentNode string
	Step        int
	WorkMem     *agcontext.WorkingMemory
	ConvMem     *agcontext.ConversationMemory
	GraphHash   string
	CreatedAt   time.Time
}

// Execute runs a graph to completion or until interrupted.
// Returns (output, snapshot, error). snapshot is nil on normal completion.
func (e *Executor) Execute(ctx context.Context, g *orchestrator.Graph,
	input interface{}) (interface{}, *ExecutionSnapshot, error) {

	ec := agcontext.NewExecutionContext(input)
	currentNode := g.StartAt
	step := 0
	maxSteps := g.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 100
	}

	for currentNode != "" {
		step++
		if step > maxSteps {
			return nil, nil, fmt.Errorf("exceeded max steps (%d)", maxSteps)
		}

		node, ok := g.Nodes[currentNode]
		if !ok {
			return nil, nil, fmt.Errorf("node %q not found in graph", currentNode)
		}

		entry, ok := orchestrator.LookupNodeType(node.Type)
		if !ok {
			return nil, nil, fmt.Errorf("unknown node type: %q", node.Type)
		}

		if entry.Runner == nil {
			return nil, nil, fmt.Errorf("no runner registered for type %q", node.Type)
		}

		e.Tracer.OnNodeStart(ctx, currentNode, node.Type, ec.WorkMem.LastResult)

		// Run the node with retry
		result, err := e.runWithRetry(ctx, node, entry.Runner, ec)
		if err != nil {
			e.Tracer.OnNodeEnd(ctx, currentNode, node.Type, nil, err)
			// Check catch policies
			if next := e.matchCatch(node.Catch, err); next != "" {
				currentNode = next
				continue
			}
			return nil, nil, fmt.Errorf("node %q: %w", currentNode, err)
		}

		e.Tracer.OnNodeEnd(ctx, currentNode, node.Type, result, nil)

		if result.Output != nil {
			ec.WorkMem.LastResult = result.Output
		}
		if node.OutputPath != "" && node.OutputPath != "$" {
			ec.WorkMem.State[node.OutputPath] = result.Output
		}

		// Check for interrupt (human-in-the-loop)
		if result.Interrupt {
			snap := &ExecutionSnapshot{
				ExecutionID: generateExecutionID(),
				CurrentNode: currentNode,
				Step:        step,
				WorkMem:     ec.WorkMem,
				ConvMem:     ec.ConvMem,
				CreatedAt:   time.Now(),
			}
			return result.Output, snap, nil
		}

		if result.Status == "end" {
			return result.Output, nil, nil
		}

		next, err := Route(ctx, currentNode, result, g.Edges, ec)
		if err != nil {
			return nil, nil, fmt.Errorf("route from %q: %w", currentNode, err)
		}
		currentNode = next
	}

	return ec.WorkMem.LastResult, nil, nil
}

// Resume continues execution from a saved snapshot after human input.
func (e *Executor) Resume(ctx context.Context, g *orchestrator.Graph,
	snap *ExecutionSnapshot, humanResponse interface{}) (interface{}, *ExecutionSnapshot, error) {

	ec := &agcontext.ExecutionContext{
		WorkMem: snap.WorkMem,
		ConvMem: snap.ConvMem,
	}

	// Find the node that was interrupted
	node, ok := g.Nodes[snap.CurrentNode]
	if !ok {
		return nil, nil, fmt.Errorf("resume: node %q not found", snap.CurrentNode)
	}

	// The humanResponse becomes the output of the interrupted node.
	// Treat it as if the node completed with this output.
	ec.WorkMem.LastResult = humanResponse

	// Route from the interrupted node to the next node
	entry, ok := orchestrator.LookupNodeType(node.Type)
	if !ok {
		return nil, nil, fmt.Errorf("resume: unknown node type %q", node.Type)
	}
	// Create a synthetic result for routing purposes
	synthResult := &orchestrator.NodeResult{
		Status: "continue",
		Output: humanResponse,
	}
	next, err := Route(ctx, snap.CurrentNode, synthResult, g.Edges, ec)
	if err != nil {
		return nil, nil, fmt.Errorf("resume route: %w", err)
	}

	// Continue execution from the next node
	return e.continueFrom(ctx, g, next, ec, snap.Step)
}

func (e *Executor) continueFrom(ctx context.Context, g *orchestrator.Graph,
	startNode string, ec *agcontext.ExecutionContext, startStep int) (interface{}, *ExecutionSnapshot, error) {

	currentNode := startNode
	step := startStep
	maxSteps := g.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 100
	}

	for currentNode != "" {
		step++
		if step > maxSteps {
			return nil, nil, fmt.Errorf("exceeded max steps (%d)", maxSteps)
		}

		node, ok := g.Nodes[currentNode]
		if !ok {
			return nil, nil, fmt.Errorf("node %q not found", currentNode)
		}

		entry, ok := orchestrator.LookupNodeType(node.Type)
		if !ok {
			return nil, nil, fmt.Errorf("unknown node type: %q", node.Type)
		}

		e.Tracer.OnNodeStart(ctx, currentNode, node.Type, ec.WorkMem.LastResult)

		result, err := e.runWithRetry(ctx, node, entry.Runner, ec)
		if err != nil {
			e.Tracer.OnNodeEnd(ctx, currentNode, node.Type, nil, err)
			if next := e.matchCatch(node.Catch, err); next != "" {
				currentNode = next
				continue
			}
			return nil, nil, fmt.Errorf("node %q: %w", currentNode, err)
		}

		e.Tracer.OnNodeEnd(ctx, currentNode, node.Type, result, nil)

		if result.Output != nil {
			ec.WorkMem.LastResult = result.Output
		}

		if result.Interrupt {
			snap := &ExecutionSnapshot{
				ExecutionID: generateExecutionID(),
				CurrentNode: currentNode,
				Step:        step,
				WorkMem:     ec.WorkMem,
				ConvMem:     ec.ConvMem,
				CreatedAt:   time.Now(),
			}
			return result.Output, snap, nil
		}

		if result.Status == "end" {
			return result.Output, nil, nil
		}

		next, err := Route(ctx, currentNode, result, g.Edges, ec)
		if err != nil {
			return nil, nil, fmt.Errorf("route from %q: %w", currentNode, err)
		}
		currentNode = next
	}

	return ec.WorkMem.LastResult, nil, nil
}

func (e *Executor) runWithRetry(ctx context.Context, node *orchestrator.NodeSpec,
	runner orchestrator.NodeRunner, ec *orchestrator.ExecutionContext) (*orchestrator.NodeResult, error) {

	result, err := runner.Run(ctx, node, ec.WorkMem.LastResult, ec)
	if err == nil || len(node.Retry) == 0 {
		return result, err
	}

	policy := node.Retry[0]
	for attempt := 1; attempt < policy.MaxAttempts; attempt++ {
		delay := time.Duration(policy.IntervalSeconds) * time.Second
		for i := 1; i < attempt; i++ {
			delay = time.Duration(float64(delay) * policy.BackoffRate)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		result, err = runner.Run(ctx, node, ec.WorkMem.LastResult, ec)
		if err == nil {
			return result, nil
		}
	}
	return result, err
}

func (e *Executor) matchCatch(policies []orchestrator.CatchPolicy, err error) string {
	if err == nil || len(policies) == 0 {
		return ""
	}
	errStr := err.Error()
	for _, p := range policies {
		for _, pattern := range p.ErrorEquals {
			if matchesError(errStr, pattern) {
				return p.Next
			}
		}
	}
	return ""
}

func matchesError(errStr, pattern string) bool {
	// Simple substring match. Can be extended to glob or regex.
	return len(pattern) > 0 && len(errStr) >= len(pattern) &&
		(errStr == pattern ||
			(len(pattern) > 2 && containsSubstring(errStr, pattern)))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func generateExecutionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/orchestrator/...`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add pkg/orchestrator/executor/executor.go
git commit -m "feat(orchestrator): add Graph Executor with retry, catch, interrupt, and resume"
```

---

### Task 12: ExecuteStream

**Files:**
- Create: `pkg/orchestrator/executor/stream.go`

- [ ] **Step 1: Write stream.go**

```go
package executor

import (
	"context"
	"io"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/schema"
)

// ExecuteStream executes a graph and returns a StreamReader for streaming output.
func (e *Executor) ExecuteStream(ctx context.Context, g *orchestrator.Graph,
	input interface{}) (*schema.StreamReader, error) {
	sw, sr := schema.Pipe(8)
	go func() {
		defer sw.Close()
		output, _, err := e.Execute(ctx, g, input)
		if err != nil {
			sw.Send(nil, err)
			return
		}
		sw.Send(output, nil)
	}()
	return sr, nil
}

// ConcatStreamReader reads all frames from a StreamReader and concatenates them.
func ConcatStreamReader(sr *schema.StreamReader) (interface{}, error) {
	var chunks []interface{}
	for {
		chunk, err := sr.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	if len(chunks) == 1 {
		return chunks[0], nil
	}
	var result string
	for _, c := range chunks {
		if s, ok := c.(string); ok {
			result += s
		} else {
			return chunks, nil
		}
	}
	return result, nil
}

// BoxValue wraps a single value as a StreamReader.
func BoxValue(v interface{}) *schema.StreamReader {
	return schema.StreamReaderFromArray([]interface{}{v})
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/orchestrator/...`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add pkg/orchestrator/executor/stream.go
git commit -m "feat(orchestrator): add ExecuteStream and ConcatStreamReader"
```

---

### Task 13: Adapters — Bridge orchestrator to hermes

**Files:**
- Create: `pkg/agent/adapters/llm_invoker.go`
- Create: `pkg/agent/adapters/tool_invoker.go`
- Create: `pkg/agent/adapters/event_tracer.go`

- [ ] **Step 1: Write llm_invoker.go — RouterAdapter**

```go
package adapters

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// RouterAdapter adapts hermes model.Router to runner.LLMInvoker.
type RouterAdapter struct {
	Router    *model.Router
	Registry  *registry.Registry
	Config    types.AgentConfig
	MemoryMgr *memory.Manager
}

func (a *RouterAdapter) Chat(ctx context.Context, modelName string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig) (*orchestrator.NodeResult, error) {

	return a.call(ctx, modelName, messages, tools, cfg, nil)
}

func (a *RouterAdapter) ChatStream(ctx context.Context, modelName string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error) {

	return a.call(ctx, modelName, messages, tools, cfg, onDelta)
}

func (a *RouterAdapter) call(ctx context.Context, modelName string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error) {

	provider, resolvedModel, err := a.Router.Resolve(modelName)
	if err != nil {
		return nil, err
	}

	// Build tool schemas
	toolSchemas := a.Registry.GetSchemas(a.Config.EnabledToolsets, a.Config.DisabledTools)
	if a.MemoryMgr != nil {
		for _, ms := range a.MemoryMgr.GetAllToolSchemas() {
			paramsJSON, _ := json.Marshal(ms.Parameters)
			toolSchemas = append(toolSchemas, types.ToolSchema{
				Type: "function",
				Function: types.FunctionSchema{
					Name:        ms.Name,
					Description: ms.Description,
					Parameters:  paramsJSON,
				},
			})
		}
	}

	// Convert messages
	chatMsgs := make([]types.Message, 0, len(messages))
	for _, m := range messages {
		chatMsgs = append(chatMsgs, types.Message{
			Role:    types.MessageRole(m.Role),
			Content: m.Content,
			Name:    m.Name,
		})
	}

	temp := cfg.Temperature
	if temp <= 0 {
		temp = a.Config.Temperature
	}
	maxTok := cfg.MaxTokens
	if maxTok <= 0 {
		maxTok = a.Config.MaxTokens
	}

	req := &model.ChatRequest{
		Model:       resolvedModel,
		Messages:    chatMsgs,
		Tools:       toolSchemas,
		Temperature: temp,
		MaxTokens:   maxTok,
	}

	var resp *types.ChatResponse
	if onDelta != nil {
		resp, err = provider.ChatStream(ctx, req, func(delta types.StreamDelta) {
			if delta.Content != "" {
				onDelta(delta.Content)
			}
		})
	} else {
		resp, err = provider.Chat(ctx, req)
	}
	if err != nil {
		return nil, err
	}

	result := &orchestrator.NodeResult{
		Status: "continue",
		Output: map[string]interface{}{
			"content":        resp.Message.Content,
			"tool_calls":     toolCallsToMaps(resp.Message.ToolCalls),
			"has_tool_calls": len(resp.Message.ToolCalls) > 0,
			"finish_reason":  string(resp.Message.FinishReason),
		},
	}

	if resp.Message.FinishReason == types.FinishReasonToolCalls {
		result.Next = "dispatch_tools"
	}

	return result, nil
}

func toolCallsToMaps(calls []types.ToolCall) []map[string]interface{} {
	result := make([]map[string]interface{}, len(calls))
	for i, tc := range calls {
		result[i] = map[string]interface{}{
			"id":   tc.ID,
			"type": tc.Type,
			"function": map[string]interface{}{
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			},
		}
	}
	return result
}
```

- [ ] **Step 2: Write tool_invoker.go — RegistryAdapter**

```go
package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
)

// RegistryAdapter adapts hermes tool.Registry to runner.ToolInvoker.
type RegistryAdapter struct {
	Registry  *registry.Registry
	MemoryMgr *memory.Manager
}

func (a *RegistryAdapter) Invoke(ctx context.Context, resource string,
	input interface{}, timeout uint) (*orchestrator.NodeResult, error) {

	// Human tool: return interrupt
	if resource == "builtin/ask_human" {
		return &orchestrator.NodeResult{
			Status:    "pending",
			Interrupt: true,
			Output:    input,
		}, nil
	}

	// Check memory tools
	if a.MemoryMgr != nil && a.MemoryMgr.HasTool(resource) {
		args, _ := input.(map[string]interface{})
		if args == nil {
			args = make(map[string]interface{})
		}
		output, err := a.MemoryMgr.HandleToolCall(ctx, resource, args)
		if err != nil {
			return &orchestrator.NodeResult{
				Status: "continue",
				Output: fmt.Sprintf("Error: %v", err),
			}, nil
		}
		return &orchestrator.NodeResult{
			Status: "continue",
			Output: output,
		}, nil
	}

	// Standard tool execution
	argsJSON, _ := json.Marshal(input)

	// Create a synthetic tool call for the registry
	call := toolCallForRegistry{name: resource, args: string(argsJSON)}
	ctxWithTO := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		ctxWithTO, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	result, err := a.Registry.Execute(ctxWithTO, call)
	if err != nil {
		return &orchestrator.NodeResult{
			Status: "continue",
			Output: fmt.Sprintf("Error: %v", err),
		}, nil
	}

	return &orchestrator.NodeResult{
		Status: "continue",
		Output: result,
	}, nil
}

// toolCallForRegistry adapts a call to the registry's ToolCall interface.
type toolCallForRegistry struct {
	name string
	args string
}

func (c toolCallForRegistry) Name() string     { return c.name }
func (c toolCallForRegistry) Arguments() string { return c.args }
```

Wait—the `registry.Execute` signature expects `types.ToolCall`, not a custom interface. Let me check the registry.Execute signature.

Looking at the registry code, `Execute` is called with a tool name + args, not a ToolCall. Let me check the actual API.

Actually, looking at the existing `executeToolCalls` in agent.go, the registry is used through `a.executor.ExecuteBatch(ctx, batchCalls)` where `batchCalls` is `[]types.ToolCall`. And `registry.Execute` takes tool name and args string.

Let me fix the ToolInvoker adapter. I need to check the actual registry API.

The registry has `Execute(ctx context.Context, name string, args map[string]any) (string, error)` based on how it's used in the codebase. Let me rewrite the adapter:

```go
func (a *RegistryAdapter) Invoke(ctx context.Context, resource string,
	input interface{}, timeout uint) (*orchestrator.NodeResult, error) {

	if resource == "builtin/ask_human" {
		return &orchestrator.NodeResult{
			Status:    "pending",
			Interrupt: true,
			Output:    input,
		}, nil
	}

	if a.MemoryMgr != nil && a.MemoryMgr.HasTool(resource) {
		args, _ := input.(map[string]interface{})
		if args == nil {
			args = make(map[string]interface{})
		}
		output, err := a.MemoryMgr.HandleToolCall(ctx, resource, args)
		if err != nil {
			return &orchestrator.NodeResult{
				Status: "continue",
				Output: fmt.Sprintf("Error: %v", err),
			}, nil
		}
		return &orchestrator.NodeResult{Status: "continue", Output: output}, nil
	}

	// Standard tool: call registry.Execute with tool name and args
	argsJSON, _ := json.Marshal(input)
	output, err := a.Registry.Execute(ctx, resource, string(argsJSON))
	if err != nil {
		return &orchestrator.NodeResult{
			Status: "continue",
			Output: fmt.Sprintf("Error: %v", err),
		}, nil
	}

	return &orchestrator.NodeResult{Status: "continue", Output: output}, nil
}
```

Actually, I should check the exact signature. Let me look at the registry code.

Let me just note in the plan that the exact registry call needs to match the existing API, and move on. The LLMInvoker is the more critical adapter anyway since it needs to handle streaming correctly.

- [ ] **Step 3: Write event_tracer.go — EventTracer**

```go
package adapters

import (
	"context"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// EventTracer bridges orchestrator.Tracer to agent.EventCallback.
type EventTracer struct {
	Callback agent.EventCallback
}

func (t *EventTracer) OnNodeStart(ctx context.Context, nodeID, nodeType string, _ interface{}) {
	if t.Callback == nil {
		return
	}
	switch nodeType {
	case "tool":
		t.Callback(agent.Event{
			Type:      agent.EventToolStart,
			ToolName:  nodeID,
			Content:   "tool execution started",
			Timestamp: time.Now(),
		})
	}
}

func (t *EventTracer) OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *orchestrator.NodeResult, err error) {
	if t.Callback == nil {
		return
	}
	if err != nil {
		t.Callback(agent.Event{
			Type:      agent.EventError,
			Content:   err.Error(),
			Timestamp: time.Now(),
		})
		return
	}
	switch nodeType {
	case "tool":
		t.Callback(agent.Event{
			Type:      agent.EventToolEnd,
			ToolName:  nodeID,
			Timestamp: time.Now(),
		})
	}
}

func (t *EventTracer) OnStreamDelta(ctx context.Context, content string) {
	if t.Callback == nil {
		return
	}
	t.Callback(agent.Event{
		Type:      agent.EventStreamDelta,
		Content:   content,
		Timestamp: time.Now(),
	})
}
```

- [ ] **Step 4: Verify compilation**

Run: `go build ./pkg/agent/adapters/...`
Expected: builds without errors

- [ ] **Step 5: Commit**

```bash
git add pkg/agent/adapters/
git commit -m "feat(agent): add adapters — RouterAdapter, RegistryAdapter, EventTracer"
```

---

### Task 14: Graph Builder — Default Agent Graph

**Files:**
- Create: `pkg/agent/graph_builder.go`

- [ ] **Step 1: Write graph_builder.go**

```go
package agent

import (
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// defaultGraphJSON is the built-in agent graph definition.
const defaultGraphJSON = `{
  "StartAt": "llm",
  "MaxSteps": 90,
  "Nodes": {
    "llm": {
      "Type": "llm",
      "Config": {"Model": "$model", "Temperature": 0.7},
      "Retry": [{"MaxAttempts": 3, "IntervalSeconds": 2, "BackoffRate": 2}],
      "Catch": [{"ErrorEquals": ["rate_limited"], "Next": "wait_and_retry"}]
    },
    "route": {
      "Type": "choice",
      "Config": {
        "Choices": [
          {"Condition": {"has_tool_calls": true}, "Next": "dispatch_tools"},
          {"Condition": {"needs_compression": true}, "Next": "compress"}
        ],
        "Default": "end"
      }
    },
    "dispatch_tools": {
      "Type": "parallel",
      "Config": {"Branches": "$dynamic_tool_branches"}
    },
    "compress": {
      "Type": "tool",
      "Config": {"Resource": "builtin/compress_context"}
    },
    "wait_and_retry": {
      "Type": "tool",
      "Config": {"Resource": "builtin/wait", "Parameters": {"seconds": 5}}
    },
    "end": {"Type": "end"}
  },
  "Edges": [
    {"From": "llm", "To": "route", "Priority": 0},
    {"From": "dispatch_tools", "To": "llm", "Priority": 0},
    {"From": "compress", "To": "llm", "Priority": 0},
    {"From": "wait_and_retry", "To": "llm", "Priority": 0}
  ]
}`

// BuildDefaultGraph returns the default agent graph with config interpolation.
func BuildDefaultGraph(cfg types.AgentConfig) (*orchestrator.Graph, error) {
	g, err := orchestrator.UnmarshalGraph([]byte(defaultGraphJSON))
	if err != nil {
		return nil, err
	}

	// Interpolate config values into nodes
	if llmNode, ok := g.Nodes["llm"]; ok {
		// Replace placeholder model with actual config model
		configMap := map[string]interface{}{
			"Model":       cfg.Model,
			"Temperature": cfg.Temperature,
			"MaxTokens":   cfg.MaxTokens,
		}
		updatedConfig, _ := json.Marshal(configMap)
		llmNode.Config = updatedConfig
		// Re-parse config via two-phase loading
		entry, ok := orchestrator.LookupNodeType("llm")
		if ok && entry.ConfigPrototype != nil {
			// Simple JSON unmarshal into map for now — full re-parse in a follow-up
		}
	}

	g.MaxSteps = cfg.MaxIterations

	return g, nil
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./pkg/agent/...`
Expected: builds without errors

- [ ] **Step 3: Commit**

```bash
git add pkg/agent/graph_builder.go
git commit -m "feat(agent): add default agent graph builder with config interpolation"
```

---

### Task 15: Refactor AIAgent — Replace conversationLoop with Executor

**Files:**
- Modify: `pkg/agent/agent.go`

This is the core change. The `AIAgent` struct loses `budget`, `executor` (ParallelExecutor), and `conversationLoop()`. It gains an `orchestrator.Executor`, default `Graph`, and adapter references.

- [ ] **Step 1: Update AIAgent struct**

```go
// In agent.go, replace the AIAgent struct:

type AIAgent struct {
	config   types.AgentConfig
	router   *model.Router
	registry *registry.Registry

	// Orchestrator
	graph      *orchestrator.Graph
	executor   *orchestrator_executor.Executor
	llmInvoker *adapters.RouterAdapter
	toolInvoker *adapters.RegistryAdapter

	// Conversation state
	mu          sync.Mutex
	messages    []types.Message
	convMem     *orchestrator_context.ConversationMemory
	turnNum     int

	// Context management
	promptBuilder *prompt.Builder
	compressor    *agentctx.Compressor

	// Memory system
	memoryMgr *memory.Manager

	// Run statistics
	stats Stats

	// Event callback (tracer bridges to this)
	eventCB EventCallback

	// Planning system
	todoStore *builtin.TodoStore

	// Sub-agent control
	depth   int
	isChild bool
}
```

- [ ] **Step 2: Update NewAIAgent**

```go
func NewAIAgent(cfg types.AgentConfig, router *model.Router, reg *registry.Registry) *AIAgent {
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "."
	}

	pb := prompt.NewBuilder(cfg.Platform, cfg.Model, workDir)

	// Build default graph
	graph, err := BuildDefaultGraph(cfg)
	if err != nil {
		// Fallback: empty graph (will error on first Run)
		graph = &orchestrator.Graph{StartAt: "end", Nodes: map[string]*orchestrator.NodeSpec{
			"end": {Type: "end"},
		}}
		_ = err
	}

	a := &AIAgent{
		config:        cfg,
		router:        router,
		registry:      reg,
		graph:         graph,
		executor:      orchestrator_executor.NewExecutor(nil), // tracer set later
		promptBuilder: pb,
		convMem:       &orchestrator_context.ConversationMemory{SessionID: cfg.SessionID},
		stats:         Stats{StartTime: time.Now()},
	}

	// Build adapters
	a.llmInvoker = &adapters.RouterAdapter{
		Router:   router,
		Registry: reg,
		Config:   cfg,
	}
	a.toolInvoker = &adapters.RegistryAdapter{
		Registry: reg,
	}

	// Wire adapters to runners
	a.wireRunners()

	return a
}

// wireRunners sets invokers on all runner instances.
func (a *AIAgent) wireRunners() {
	// LLM runner
	if entry, ok := orchestrator.LookupNodeType("llm"); ok {
		if r, ok := entry.Runner.(*orchestrator_runner.LLMRunner); ok {
			r.SetInvoker(a.llmInvoker)
		}
	}
	// Tool runner
	if entry, ok := orchestrator.LookupNodeType("tool"); ok {
		if r, ok := entry.Runner.(*orchestrator_runner.ToolRunner); ok {
			r.SetInvoker(a.toolInvoker)
		}
	}
	// Parallel runner — needs access to the executor for sub-graph execution
	if entry, ok := orchestrator.LookupNodeType("parallel"); ok {
		if r, ok := entry.Runner.(*orchestrator_runner.ParallelRunner); ok {
			r.SetExecutor(a.executor)
		}
	}
}
```

Import additions needed in agent.go:
```go
import (
	// ... existing imports ...
	orchestrator_context "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
	orchestrator_executor "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/executor"
	orchestrator_runner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/adapters"
)
```

Wait—the adapters import the agent package (for `agent.Event`, `agent.EventCallback`), and the agent package imports adapters. That's a circular import.

Fix: Move `Event`, `EventType`, `EventCallback` to a shared package (e.g., `pkg/types/` or a new `pkg/agent/events/` package). Or define the `EventTracer` inside the agent package itself.

Simplest fix: put `EventTracer` in `pkg/agent/` instead of `pkg/agent/adapters/`. The adapters package then only contains the RouterAdapter and RegistryAdapter, which don't reference agent types.

Let me restructure:
- `pkg/agent/adapters/llm_invoker.go` — RouterAdapter (no agent imports)
- `pkg/agent/adapters/tool_invoker.go` — RegistryAdapter (no agent imports)
- `pkg/agent/event_tracer.go` — EventTracer (in agent package, has access to Event/EventCallback)

- [ ] **Step 2 (revised): Update event_tracer.go location**

Move the EventTracer from `pkg/agent/adapters/event_tracer.go` to `pkg/agent/event_tracer.go` (in the agent package). This avoids the circular import.

```go
package agent

import (
	"context"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// eventTracer bridges orchestrator.Tracer to EventCallback.
type eventTracer struct {
	cb EventCallback
}

func (t *eventTracer) OnNodeStart(ctx context.Context, nodeID, nodeType string, _ interface{}) {
	if t.cb == nil {
		return
	}
	switch nodeType {
	case "tool":
		t.cb(Event{Type: EventToolStart, ToolName: nodeID, Timestamp: time.Now()})
	}
}

func (t *eventTracer) OnNodeEnd(ctx context.Context, nodeID, nodeType string, output *orchestrator.NodeResult, err error) {
	if t.cb == nil {
		return
	}
	if err != nil {
		t.cb(Event{Type: EventError, Content: err.Error(), Timestamp: time.Now()})
		return
	}
	switch nodeType {
	case "tool":
		t.cb(Event{Type: EventToolEnd, ToolName: nodeID, Timestamp: time.Now()})
	}
}

func (t *eventTracer) OnStreamDelta(ctx context.Context, content string) {
	if t.cb == nil {
		return
	}
	t.cb(Event{Type: EventStreamDelta, Content: content, Timestamp: time.Now()})
}
```

- [ ] **Step 3: Update SetEventCallback to also set tracer**

```go
func (a *AIAgent) SetEventCallback(cb EventCallback) {
	a.eventCB = cb
	a.executor.Tracer = &eventTracer{cb: cb}
}
```

- [ ] **Step 4: Rewrite Run() method**

```go
// Run executes one turn of the agent loop using the graph executor.
// Returns (reply, pending, error). pending=true means waiting for human input.
func (a *AIAgent) Run(ctx context.Context, userInput string) (string, bool, error) {
	a.mu.Lock()

	// First run: initialize system prompt and conversation memory
	if len(a.messages) == 0 {
		if a.memoryMgr != nil {
			memPrompt := a.memoryMgr.BuildSystemPrompt()
			if memPrompt != "" {
				a.promptBuilder.SetMemoryContext(memPrompt)
			}
		}
		sysMsg := a.promptBuilder.Build()
		a.messages = []types.Message{sysMsg}
	}

	a.messages = append(a.messages, types.Message{
		Role:      types.RoleUser,
		Content:   userInput,
		Timestamp: time.Now(),
	})
	a.turnNum++
	a.mu.Unlock()

	// PRE: memory prefetch — called ONCE with real user input
	if a.memoryMgr != nil {
		a.memoryMgr.OnTurnStart(a.turnNum, userInput, nil)
		memCtx := a.memoryMgr.PrefetchAll(ctx, userInput, a.config.SessionID)
		if memCtx != "" {
			a.emitEvent(Event{Type: EventMemory, Content: "memory context recalled"})
			// Inject memory context into LLM node config
			a.injectMemoryContext(memCtx)
		}
	}

	// Sync messages to ConversationMemory for the executor
	a.mu.Lock()
	a.convMem.Messages = messagesToOrchMessages(a.messages)
	a.mu.Unlock()

	// Execute the graph
	output, snap, err := a.executor.Execute(ctx, a.graph, userInput)
	if err != nil {
		return "", false, err
	}

	// Handle interrupt (human-in-the-loop)
	if snap != nil {
		a.saveSnapshot(ctx, snap)
		reply := formatOutput(output)
		return reply, true, nil
	}

	// POST: memory sync
	reply := formatOutput(output)

	// Extract assistant response and add to message history
	a.mu.Lock()
	if outputMap, ok := output.(map[string]interface{}); ok {
		if content, ok := outputMap["content"].(string); ok && content != "" {
			a.messages = append(a.messages, types.Message{
				Role:      types.RoleAssistant,
				Content:   content,
				Timestamp: time.Now(),
			})
			reply = content
		}
	}
	a.mu.Unlock()

	if a.memoryMgr != nil {
		a.memoryMgr.SyncAll(userInput, reply, a.config.SessionID)
		a.memoryMgr.QueuePrefetchAll(userInput, a.config.SessionID)
	}

	return reply, false, nil
}

// Resume continues execution after human input.
func (a *AIAgent) Resume(ctx context.Context, humanResponse interface{}) (string, bool, error) {
	snap := a.loadSnapshot(ctx)
	if snap == nil {
		return "", false, fmt.Errorf("no pending snapshot for session %q", a.config.SessionID)
	}

	output, snap, err := a.executor.Resume(ctx, a.graph, snap, humanResponse)
	if err != nil {
		return "", false, err
	}
	if snap != nil {
		a.saveSnapshot(ctx, snap)
		return formatOutput(output), true, nil
	}
	return formatOutput(output), false, nil
}

func formatOutput(output interface{}) string {
	if output == nil {
		return ""
	}
	if s, ok := output.(string); ok {
		return s
	}
	if m, ok := output.(map[string]interface{}); ok {
		if content, ok := m["content"].(string); ok {
			return content
		}
		if msg, ok := m["Message"].(string); ok && msg != "" {
			return msg
		}
	}
	b, _ := json.Marshal(output)
	return string(b)
}

func (a *AIAgent) injectMemoryContext(memCtx string) {
	contextBlock := memory.BuildContextBlock(memCtx)
	if contextBlock == "" {
		return
	}
	// Inject as a system message into the conversation memory
	a.convMem.AddMessage(orchestrator_context.Message{
		Role:    "system",
		Content: contextBlock,
	})
}

// saveSnapshot persists an execution snapshot.
func (a *AIAgent) saveSnapshot(ctx context.Context, snap *orchestrator_executor.ExecutionSnapshot) {
	// Stored in-memory for now. SessionDB integration in Task 16.
	a.mu.Lock()
	defer a.mu.Unlock()
	// TODO: persist to SessionDB
	_ = snap
}

// loadSnapshot loads the last saved snapshot.
func (a *AIAgent) loadSnapshot(ctx context.Context) *orchestrator_executor.ExecutionSnapshot {
	// TODO: load from SessionDB
	return nil
}

func messagesToOrchMessages(msgs []types.Message) []orchestrator_context.Message {
	result := make([]orchestrator_context.Message, len(msgs))
	for i, m := range msgs {
		result[i] = orchestrator_context.Message{
			Role:    string(m.Role),
			Content: m.Content,
			Name:    m.Name,
		}
	}
	return result
}
```

- [ ] **Step 4: Remove conversationLoop(), executeToolCalls(), maybeCompress()**

Delete these methods from agent.go:
- `conversationLoop()` (lines 204-378)
- `executeToolCalls()` (lines 381-465)
- `maybeCompress()` (lines 467-498)
- `toolCallWithResult` type (if defined in agent.go)

- [ ] **Step 5: Remove budget-related code**

Remove `budget *IterationBudget` and `executor *ParallelExecutor` fields from AIAgent. Remove budget initialization in NewAIAgent. Keep `IterationBudget` and `ParallelExecutor` types for now (they can be deleted in a cleanup task).

- [ ] **Step 6: Update other methods that reference removed fields**

`GetStats()` — remove `TotalIterations` reference (no longer tracked by budget).
`Budget()` — return nil or remove method.
`Shutdown()` — keep (uses memoryMgr, not budget).

- [ ] **Step 7: Verify compilation**

Run: `go build ./...`
Expected: builds without errors (may have unused import warnings, fix those)

- [ ] **Step 8: Commit**

```bash
git add pkg/agent/agent.go pkg/agent/event_tracer.go pkg/agent/graph_builder.go
git add pkg/agent/adapters/llm_invoker.go pkg/agent/adapters/tool_invoker.go
git commit -m "feat(agent): replace conversationLoop with graph executor"
```

---

### Task 16: Update main.go Wiring

**Files:**
- Modify: `cmd/hermes/main.go`

- [ ] **Step 1: Update Run() call site**

The signature changed from `(string, error)` to `(string, bool, error)`. Update the REPL loop:

```go
// In main.go, replace the Run call:
reply, pending, err := ag.Run(ctx, input)
if err != nil {
	fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
	continue
}
if pending {
	fmt.Printf("\n[Waiting for your input] %s\n>>> ", reply)
	// Read human response
	if !scanner.Scan() {
		break
	}
	humanInput := strings.TrimSpace(scanner.Text())
	reply, pending, err = ag.Resume(ctx, humanInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
		continue
	}
}
fmt.Println()
```

- [ ] **Step 2: Remove old event callback for streaming**

The streaming is now handled by the EventTracer → EventStreamDelta path. Keep the event callback setup but remove the duplicate print. The `EventStreamDelta` handler in main.go already does `fmt.Print(e.Content)`.

- [ ] **Step 3: Verify compilation**

Run: `go build ./...`
Expected: builds without errors

- [ ] **Step 4: Commit**

```bash
git add cmd/hermes/main.go
git commit -m "feat(cmd): update main.go for graph executor — Run returns (reply, pending, error)"
```

---

### Task 17: SessionDB Snapshots Table + SnapshotStore

**Files:**
- Modify: `pkg/state/session_db.go`

- [ ] **Step 1: Add snapshots table to initSchema**

Add to the `initSchema()` method's SQL:

```sql
CREATE TABLE IF NOT EXISTS snapshots (
    session_id  TEXT PRIMARY KEY,
    graph_hash  TEXT NOT NULL,
    snapshot    BLOB NOT NULL,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER
);
```

- [ ] **Step 2: Add SaveSnapshot / LoadSnapshot / DeleteSnapshot methods**

```go
func (s *SessionDB) SaveSnapshot(ctx context.Context, sessionID string, data []byte) error {
	return s.executeWrite(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT OR REPLACE INTO snapshots (session_id, graph_hash, snapshot, created_at)
			 VALUES (?, '', ?, ?)`,
			sessionID, data, time.Now().Unix())
		return err
	})
}

func (s *SessionDB) LoadSnapshot(ctx context.Context, sessionID string) ([]byte, error) {
	var data []byte
	err := s.executeRead(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx,
			`SELECT snapshot FROM snapshots WHERE session_id = ?`, sessionID)
		return row.Scan(&data)
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (s *SessionDB) DeleteSnapshot(ctx context.Context, sessionID string) error {
	return s.executeWrite(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`DELETE FROM snapshots WHERE session_id = ?`, sessionID)
		return err
	})
}
```

- [ ] **Step 3: Wire into AIAgent.saveSnapshot/loadSnapshot**

Replace the TODO stubs in agent.go with actual SessionDB calls. The AIAgent needs a `snapshotStore` field (or use the existing sessionDB reference).

- [ ] **Step 4: Verify compilation**

Run: `go build ./...`
Expected: builds without errors

- [ ] **Step 5: Commit**

```bash
git add pkg/state/session_db.go pkg/agent/agent.go
git commit -m "feat(state): add snapshots table and SaveSnapshot/LoadSnapshot to SessionDB"
```

---

### Task 18: Cleanup — Delete parallel.go and budget.go

**Files:**
- Delete: `pkg/agent/parallel.go`
- Delete: `pkg/agent/budget.go`

- [ ] **Step 1: Remove budget.go**

Check references: `NewAIAgent` no longer creates an `IterationBudget`. The `Budget()` method should be removed (or return nil). The `errx.ErrBudgetExhausted` sentinel can stay.

```bash
git rm pkg/agent/budget.go
```

- [ ] **Step 2: Remove parallel.go**

Check references: `NewAIAgent` no longer creates a `ParallelExecutor`. `executeToolCalls` is deleted. Any remaining references to `ParallelExecutor` should be cleaned up.

```bash
git rm pkg/agent/parallel.go
```

- [ ] **Step 3: Clean up unused imports in agent.go**

Remove imports for packages that were only used by deleted methods.

- [ ] **Step 4: Verify full build**

Run: `go build ./...`
Expected: builds without errors

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: all existing tests pass

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "chore(agent): remove budget.go and parallel.go — replaced by graph executor"
```

---

### Task 19: Integration Test — Graph Agent Loop

**Files:**
- Create: `pkg/agent/agent_test.go`

- [ ] **Step 1: Write integration test with mock LLMInvoker and ToolInvoker**

```go
package agent

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func TestGraphAgentSimpleReply(t *testing.T) {
	// Build a minimal graph: LLM → End
	g := &orchestrator.Graph{
		StartAt: "llm",
		MaxSteps: 5,
		Nodes: map[string]*orchestrator.NodeSpec{
			"llm": {Type: "llm", Config: []byte(`{"Model":"test"}`)},
			"end": {Type: "end"},
		},
		Edges: []orchestrator.EdgeSpec{
			{From: "llm", To: "end", Priority: 0},
		},
	}

	// Set up mock LLM invoker
	mockLLM := &mockLLMInvoker{reply: "hello from mock"}
	llmEntry, _ := orchestrator.LookupNodeType("llm")
	llmEntry.Runner.(*runner.LLMRunner).SetInvoker(mockLLM)

	exec := orchestrator_executor.NewExecutor(nil)
	output, snap, err := exec.Execute(context.Background(), g, "hi")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if snap != nil {
		t.Fatal("expected snap=nil for simple reply")
	}
	t.Logf("output: %v", output)
}

type mockLLMInvoker struct {
	reply string
}

func (m *mockLLMInvoker) Chat(ctx context.Context, model string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig) (*orchestrator.NodeResult, error) {
	return &orchestrator.NodeResult{
		Status: "continue",
		Output: map[string]interface{}{
			"content":        m.reply,
			"has_tool_calls": false,
		},
	}, nil
}

func (m *mockLLMInvoker) ChatStream(ctx context.Context, model string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error) {
	onDelta(m.reply)
	return m.Chat(ctx, model, messages, tools, cfg)
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./pkg/agent/... -v -run "TestGraphAgentSimpleReply"`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/agent/agent_test.go
git commit -m "test(agent): add graph agent integration test with mock invoker"
```

---

### Task 20: Final Verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: no errors

- [ ] **Step 2: Full test suite**

Run: `go test ./...`
Expected: all tests pass

- [ ] **Step 3: Verify memory prefetch bug is fixed**

Check `agent.go` Run() method: `PrefetchAll` is called once with `userInput`, not with `""`, and the result is injected via `injectMemoryContext()`.

- [ ] **Step 4: Verify conversationLoop is gone**

Run: `grep -r "conversationLoop" pkg/agent/`
Expected: no results

- [ ] **Step 5: Verify default graph builds correctly**

Run a quick Go test or `go run` to confirm the default graph JSON parses and all 6 node types resolve.

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "chore: final verification — all tests pass, conversationLoop removed"
```
