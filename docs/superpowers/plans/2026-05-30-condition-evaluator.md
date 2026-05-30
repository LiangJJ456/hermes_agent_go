# Condition Evaluator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a pluggable condition evaluator that unifies choice-node and edge-routing condition logic, replacing the buggy map-equality matcher and wiring up the previously-ignored `EdgeSpec.Condition`.

**Architecture:** A new leaf package `pkg/orchestrator/condition` defines a `Scope` contract, an `Evaluator` interface, a swappable package-level `Default`, and a default implementation built on Go's standard `go/parser` (parse expression → walk a supported AST subset). The `choice` runner and edge `Route` both call `condition.Evaluate`. Conditions are validated at graph-load time so bad expressions fail fast.

**Tech Stack:** Go, standard library `go/parser` / `go/ast` / `go/token` (zero third-party dependencies).

**Reference spec:** `docs/superpowers/specs/2026-05-30-condition-evaluator-design.md`

**Key constraints discovered during design:**
- `pkg/orchestrator/context` is a leaf (no internal imports); `pkg/orchestrator/runner` already imports it — so the `choice` runner reading `ec.WorkMem.State` introduces no import cycle.
- `orchestrator` must NOT import `runner` (cycle). Therefore `Graph` validation handles **edge** conditions directly, and **choice-node** conditions are validated via an optional `interface{ Validate() error }` hook that `runner.ChoiceConfig` implements.
- The `condition` package imports only stdlib, so both `orchestrator` and `runner` may import it freely.
- Field keys accessed with dot syntax (`input.has_tool_calls`) must be valid Go identifiers. Keys with hyphens/spaces are out of scope for this plan (documented limitation).

---

## File Structure

| Action | File | Responsibility |
|---|---|---|
| Create | `pkg/orchestrator/condition/evaluator.go` | `Scope`, `Evaluator` interface, package-level `Default` + `Evaluate`/`Validate` helpers |
| Create | `pkg/orchestrator/condition/dsl.go` | Default `go/parser`-based implementation: AST walk + comparison/logic + `toFloat64` |
| Create | `pkg/orchestrator/condition/evaluator_test.go` | Table-driven tests for the default evaluator |
| Modify | `pkg/orchestrator/runner/choice.go` | `ChoiceEntry.Condition` → `string`; evaluate via `condition.Evaluate`; `ChoiceConfig.Validate()` |
| Modify | `pkg/orchestrator/runner/runner_test.go` | Rewrite choice tests to string-expression syntax |
| Modify | `pkg/orchestrator/executor/route.go` | Evaluate `EdgeSpec.Condition` in priority order; first match wins |
| Modify | `pkg/orchestrator/executor/route_test.go` | Add conditional-edge routing tests |
| Modify | `pkg/orchestrator/graph.go` | `EdgeSpec.Condition` → `string` |
| Modify | `pkg/orchestrator/graph_json.go` | Validate edge conditions + invoke node-config `Validate()` hook during `UnmarshalGraph` |
| Modify | `pkg/agent/graph_builder.go` | Default graph JSON uses string-expression conditions |

---

## Task 1: condition package skeleton + boolean literals

**Files:**
- Create: `pkg/orchestrator/condition/evaluator.go`
- Create: `pkg/orchestrator/condition/dsl.go`
- Create: `pkg/orchestrator/condition/evaluator_test.go`

- [ ] **Step 1: Write the failing test**

Create `pkg/orchestrator/condition/evaluator_test.go`:

```go
package condition

import "testing"

func TestEvaluate_BooleanLiterals(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"true", true},
		{"false", false},
	}
	for _, c := range cases {
		got, err := Evaluate(c.expr, Scope{})
		if err != nil {
			t.Fatalf("Evaluate(%q) error: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("Evaluate(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/condition/ -run TestEvaluate_BooleanLiterals -v`
Expected: build failure — `undefined: Evaluate` / `undefined: Scope` (package does not exist yet).

- [ ] **Step 3: Write minimal implementation**

Create `pkg/orchestrator/condition/evaluator.go`:

```go
// Package condition evaluates boolean routing conditions written as string
// expressions. The default implementation uses Go's standard expression parser;
// the Default evaluator can be swapped for a different engine without changing
// call sites or the condition string format.
package condition

// Scope is the data a condition may reference. This contract is intended to
// remain stable across evaluator implementations.
type Scope struct {
	Input interface{}            // previous node's Output (ec.WorkMem.LastResult)
	State map[string]interface{} // cross-node WorkMem.State
}

// Evaluator evaluates and validates condition expressions.
type Evaluator interface {
	Evaluate(expr string, scope Scope) (bool, error)
	Validate(expr string) error
}

// Default is the active evaluator. Replace it to swap engines, e.g.
// condition.Default = myExprLangEvaluator{}.
var Default Evaluator = newDSLEvaluator()

// Evaluate reports whether expr holds true under scope, using Default.
func Evaluate(expr string, scope Scope) (bool, error) { return Default.Evaluate(expr, scope) }

// Validate parses and type-checks expr without evaluating it, using Default.
func Validate(expr string) error { return Default.Validate(expr) }
```

Create `pkg/orchestrator/condition/dsl.go`:

```go
package condition

import (
	"fmt"
	"go/ast"
	"go/parser"
)

// undefinedT marks a value referenced in scope but not present. Comparisons
// against undefined yield false (except == / != against another undefined).
type undefinedT struct{}

var undefined = undefinedT{}

// dslEvaluator is the default evaluator: it parses an expression with go/parser
// and evaluates the supported subset of the resulting AST.
type dslEvaluator struct{}

func newDSLEvaluator() *dslEvaluator { return &dslEvaluator{} }

func (e *dslEvaluator) Evaluate(expr string, scope Scope) (bool, error) {
	node, err := parser.ParseExpr(expr)
	if err != nil {
		return false, fmt.Errorf("parse condition %q: %w", expr, err)
	}
	v, err := evalNode(node, scope)
	if err != nil {
		return false, fmt.Errorf("evaluate condition %q: %w", expr, err)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("condition %q did not evaluate to bool (got %T)", expr, v)
	}
	return b, nil
}

func (e *dslEvaluator) Validate(expr string) error {
	node, err := parser.ParseExpr(expr)
	if err != nil {
		return fmt.Errorf("parse condition %q: %w", expr, err)
	}
	return checkNode(node)
}

// evalNode evaluates a supported AST expression to a concrete value
// (bool, float64, string, or undefined).
func evalNode(n ast.Expr, scope Scope) (interface{}, error) {
	switch node := n.(type) {
	case *ast.Ident:
		return evalIdent(node, scope)
	default:
		return nil, fmt.Errorf("unsupported expression: %T", n)
	}
}

func evalIdent(node *ast.Ident, scope Scope) (interface{}, error) {
	switch node.Name {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "input":
		return scope.Input, nil
	case "state":
		return scope.State, nil
	default:
		return nil, fmt.Errorf("unknown identifier %q (expected input/state/true/false)", node.Name)
	}
}

// checkNode is filled in by Task 5. For now accept any node so Validate is a
// no-op beyond parsing; later tasks tighten it.
func checkNode(n ast.Expr) error { return nil }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/condition/ -run TestEvaluate_BooleanLiterals -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/condition/
git commit -m "feat(condition): add evaluator package skeleton with boolean literals"
```

---

## Task 2: field access (input/state) and missing-key semantics

**Files:**
- Modify: `pkg/orchestrator/condition/dsl.go`
- Test: `pkg/orchestrator/condition/evaluator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `evaluator_test.go`:

```go
func TestEvaluate_FieldAccess(t *testing.T) {
	scope := Scope{
		Input: map[string]interface{}{
			"has_tool_calls": true,
			"nested":         map[string]interface{}{"deep": true},
		},
		State: map[string]interface{}{"ready": true},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"input.has_tool_calls", true},
		{"input.nested.deep", true},
		{"state.ready", true},
		{"input.missing", false},  // missing key -> undefined -> bare bool is false
		{"state.missing", false},
	}
	for _, c := range cases {
		got, err := Evaluate(c.expr, scope)
		if err != nil {
			t.Fatalf("Evaluate(%q) error: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("Evaluate(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/condition/ -run TestEvaluate_FieldAccess -v`
Expected: FAIL — `input.has_tool_calls` is an `*ast.SelectorExpr`, currently "unsupported expression: *ast.SelectorExpr". Also a bare undefined/bool needs the truthy rule.

- [ ] **Step 3: Write minimal implementation**

In `dsl.go`, add `*ast.SelectorExpr` and `*ast.ParenExpr` cases to `evalNode`, and add `evalSelector`, `toStringMap`, and `truthy` helpers. Also make `Evaluate` treat `undefined` as `false` when the whole expression is a bare reference:

Replace the `evalNode` switch with:

```go
func evalNode(n ast.Expr, scope Scope) (interface{}, error) {
	switch node := n.(type) {
	case *ast.ParenExpr:
		return evalNode(node.X, scope)
	case *ast.Ident:
		return evalIdent(node, scope)
	case *ast.SelectorExpr:
		return evalSelector(node, scope)
	default:
		return nil, fmt.Errorf("unsupported expression: %T", n)
	}
}

func evalSelector(node *ast.SelectorExpr, scope Scope) (interface{}, error) {
	base, err := evalNode(node.X, scope)
	if err != nil {
		return nil, err
	}
	m, ok := toStringMap(base)
	if !ok {
		return undefined, nil
	}
	v, ok := m[node.Sel.Name]
	if !ok {
		return undefined, nil
	}
	return v, nil
}

func toStringMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}

// truthy interprets a value in boolean context: real bools pass through,
// everything else (including undefined) is false.
func truthy(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
```

Then change the bool coercion in `Evaluate` so a bare reference resolving to `undefined` is treated as `false` rather than an error:

```go
	b, ok := v.(bool)
	if !ok {
		if v == undefined {
			return false, nil
		}
		return false, fmt.Errorf("condition %q did not evaluate to bool (got %T)", expr, v)
	}
	return b, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/condition/ -run "TestEvaluate_FieldAccess|TestEvaluate_BooleanLiterals" -v`
Expected: PASS (both tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/condition/
git commit -m "feat(condition): support input/state field access with missing-key semantics"
```

---

## Task 3: comparison operators with numeric/string/bool normalization

**Files:**
- Modify: `pkg/orchestrator/condition/dsl.go`
- Test: `pkg/orchestrator/condition/evaluator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `evaluator_test.go`:

```go
func TestEvaluate_Comparisons(t *testing.T) {
	scope := Scope{
		Input: map[string]interface{}{
			"count":  3,        // Go int (runtime value)
			"name":   "alice",
			"flag":   true,
		},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"input.count == 3", true},   // int vs JSON-number(float64) must match
		{"input.count != 3", false},
		{"input.count > 2", true},
		{"input.count < 2", false},
		{"input.count >= 3", true},
		{"input.count <= 2", false},
		{`input.name == "alice"`, true},
		{`input.name != "bob"`, true},
		{"input.flag == true", true},
		{"input.flag == false", false},
		{"input.missing == 3", false},   // undefined vs value -> false
		{"input.missing != 3", true},    // undefined != value -> true
	}
	for _, c := range cases {
		got, err := Evaluate(c.expr, scope)
		if err != nil {
			t.Fatalf("Evaluate(%q) error: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("Evaluate(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/condition/ -run TestEvaluate_Comparisons -v`
Expected: FAIL — `input.count == 3` is an `*ast.BinaryExpr`, currently "unsupported expression: *ast.BinaryExpr".

- [ ] **Step 3: Write minimal implementation**

In `dsl.go`, add `"go/token"` to imports. Add the `*ast.BinaryExpr` and `*ast.BasicLit` cases to `evalNode`, and add `evalBinary`, `compare`, `evalLit`, and `toFloat64`:

```go
	case *ast.BinaryExpr:
		return evalBinary(node, scope)
	case *ast.BasicLit:
		return evalLit(node)
```

```go
func evalLit(node *ast.BasicLit) (interface{}, error) {
	switch node.Kind {
	case token.INT, token.FLOAT:
		f, err := strconv.ParseFloat(node.Value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number literal %q: %w", node.Value, err)
		}
		return f, nil
	case token.STRING:
		s, err := strconv.Unquote(node.Value)
		if err != nil {
			return nil, fmt.Errorf("invalid string literal %q: %w", node.Value, err)
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported literal: %s", node.Kind)
	}
}

func evalBinary(node *ast.BinaryExpr, scope Scope) (interface{}, error) {
	switch node.Op {
	case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
		l, err := evalNode(node.X, scope)
		if err != nil {
			return nil, err
		}
		r, err := evalNode(node.Y, scope)
		if err != nil {
			return nil, err
		}
		return compare(node.Op, l, r)
	default:
		return nil, fmt.Errorf("unsupported operator %q", node.Op)
	}
}

func compare(op token.Token, l, r interface{}) (bool, error) {
	// undefined: only equality is meaningful.
	if l == undefined || r == undefined {
		switch op {
		case token.EQL:
			return l == r, nil
		case token.NEQ:
			return l != r, nil
		default:
			return false, nil
		}
	}
	// numeric (cross-type via float64).
	if lf, lok := toFloat64(l); lok {
		rf, rok := toFloat64(r)
		if !rok {
			return op == token.NEQ, nil // number vs non-number: unequal
		}
		switch op {
		case token.EQL:
			return lf == rf, nil
		case token.NEQ:
			return lf != rf, nil
		case token.LSS:
			return lf < rf, nil
		case token.GTR:
			return lf > rf, nil
		case token.LEQ:
			return lf <= rf, nil
		case token.GEQ:
			return lf >= rf, nil
		}
	}
	// string (lexicographic).
	if ls, lok := l.(string); lok {
		rs, rok := r.(string)
		if !rok {
			return op == token.NEQ, nil
		}
		switch op {
		case token.EQL:
			return ls == rs, nil
		case token.NEQ:
			return ls != rs, nil
		case token.LSS:
			return ls < rs, nil
		case token.GTR:
			return ls > rs, nil
		case token.LEQ:
			return ls <= rs, nil
		case token.GEQ:
			return ls >= rs, nil
		}
	}
	// bool and everything else: equality only.
	switch op {
	case token.EQL:
		return l == r, nil
	case token.NEQ:
		return l != r, nil
	default:
		return false, fmt.Errorf("operator %q not supported for values %T and %T", op, l, r)
	}
}

// toFloat64 normalizes JSON numbers (float64) and runtime ints to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
```

Add `"strconv"` to the import block as well.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/condition/ -v`
Expected: PASS (all three tests so far).

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/condition/
git commit -m "feat(condition): add comparison operators with numeric/string normalization"
```

---

## Task 4: logical operators, NOT, and parentheses

**Files:**
- Modify: `pkg/orchestrator/condition/dsl.go`
- Test: `pkg/orchestrator/condition/evaluator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `evaluator_test.go`:

```go
func TestEvaluate_Logical(t *testing.T) {
	scope := Scope{
		Input: map[string]interface{}{
			"has_tool_calls":    true,
			"needs_compression": false,
			"count":             5,
		},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"input.has_tool_calls && input.count > 3", true},
		{"input.has_tool_calls && input.count > 9", false},
		{"input.needs_compression || input.count == 5", true},
		{"!input.needs_compression", true},
		{"!input.has_tool_calls", false},
		{"(input.has_tool_calls || input.needs_compression) && input.count < 3", false},
	}
	for _, c := range cases {
		got, err := Evaluate(c.expr, scope)
		if err != nil {
			t.Fatalf("Evaluate(%q) error: %v", c.expr, err)
		}
		if got != c.want {
			t.Fatalf("Evaluate(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/condition/ -run TestEvaluate_Logical -v`
Expected: FAIL — `&&` / `||` reach the `default` branch of `evalBinary` ("unsupported operator"), and `!` is an `*ast.UnaryExpr` ("unsupported expression").

- [ ] **Step 3: Write minimal implementation**

In `dsl.go`, add the `*ast.UnaryExpr` case to `evalNode`:

```go
	case *ast.UnaryExpr:
		return evalUnary(node, scope)
```

Add `evalUnary`, and extend `evalBinary` to handle short-circuiting logical operators. Add a `token.LAND`/`token.LOR` branch at the top of `evalBinary`'s switch:

```go
func evalUnary(node *ast.UnaryExpr, scope Scope) (interface{}, error) {
	if node.Op != token.NOT {
		return nil, fmt.Errorf("unsupported unary operator %q", node.Op)
	}
	v, err := evalNode(node.X, scope)
	if err != nil {
		return nil, err
	}
	return !truthy(v), nil
}
```

Update `evalBinary` so its switch starts with:

```go
func evalBinary(node *ast.BinaryExpr, scope Scope) (interface{}, error) {
	switch node.Op {
	case token.LAND, token.LOR:
		l, err := evalNode(node.X, scope)
		if err != nil {
			return nil, err
		}
		lb := truthy(l)
		if node.Op == token.LAND && !lb {
			return false, nil
		}
		if node.Op == token.LOR && lb {
			return true, nil
		}
		r, err := evalNode(node.Y, scope)
		if err != nil {
			return nil, err
		}
		return truthy(r), nil
	case token.EQL, token.NEQ, token.LSS, token.GTR, token.LEQ, token.GEQ:
		// ... unchanged comparison branch from Task 3 ...
	default:
		return nil, fmt.Errorf("unsupported operator %q", node.Op)
	}
}
```

(Keep the comparison branch body exactly as written in Task 3; only the `token.LAND, token.LOR` case is new.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/condition/ -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/condition/
git commit -m "feat(condition): add logical operators, NOT, and parentheses"
```

---

## Task 5: Validate rejects unsupported constructs at parse time

**Files:**
- Modify: `pkg/orchestrator/condition/dsl.go`
- Test: `pkg/orchestrator/condition/evaluator_test.go`

- [ ] **Step 1: Write the failing test**

Add to `evaluator_test.go`:

```go
func TestValidate(t *testing.T) {
	valid := []string{
		"input.has_tool_calls == true",
		"input.count > 3 && state.ready",
		`input.name != "x" || !input.flag`,
	}
	for _, expr := range valid {
		if err := Validate(expr); err != nil {
			t.Fatalf("Validate(%q) unexpected error: %v", expr, err)
		}
	}

	invalid := []string{
		"input.count +",              // parse error
		"foo(input.count)",           // function call (unsupported)
		"input.count + 1 == 4",       // arithmetic (unsupported)
		"bogus.field == 1",           // unknown root identifier
	}
	for _, expr := range invalid {
		if err := Validate(expr); err == nil {
			t.Fatalf("Validate(%q) expected error, got nil", expr)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/condition/ -run TestValidate -v`
Expected: FAIL — `checkNode` currently returns nil for everything, so the `invalid` function-call/arithmetic/unknown-identifier cases are wrongly accepted.

- [ ] **Step 3: Write minimal implementation**

In `dsl.go`, replace the placeholder `checkNode` with a real recursive checker that accepts only the supported node set:

```go
func checkNode(n ast.Expr) error {
	switch node := n.(type) {
	case *ast.ParenExpr:
		return checkNode(node.X)
	case *ast.UnaryExpr:
		if node.Op != token.NOT {
			return fmt.Errorf("unsupported unary operator %q", node.Op)
		}
		return checkNode(node.X)
	case *ast.BinaryExpr:
		switch node.Op {
		case token.LAND, token.LOR, token.EQL, token.NEQ,
			token.LSS, token.GTR, token.LEQ, token.GEQ:
		default:
			return fmt.Errorf("unsupported operator %q", node.Op)
		}
		if err := checkNode(node.X); err != nil {
			return err
		}
		return checkNode(node.Y)
	case *ast.SelectorExpr:
		// Field names (node.Sel) are arbitrary keys; only the base chain
		// must resolve to input/state.
		return checkNode(node.X)
	case *ast.Ident:
		switch node.Name {
		case "true", "false", "input", "state":
			return nil
		default:
			return fmt.Errorf("unknown identifier %q (expected input/state/true/false)", node.Name)
		}
	case *ast.BasicLit:
		switch node.Kind {
		case token.INT, token.FLOAT, token.STRING:
			return nil
		default:
			return fmt.Errorf("unsupported literal kind %s", node.Kind)
		}
	default:
		return fmt.Errorf("unsupported expression: %T", n)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/condition/ -v`
Expected: PASS (all five tests). Run `gofmt -l pkg/orchestrator/condition/` and expect no output.

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/condition/
git commit -m "feat(condition): validate expressions at parse time"
```

---

## Task 6: choice runner uses string conditions via the evaluator

**Files:**
- Modify: `pkg/orchestrator/runner/choice.go`
- Modify: `pkg/orchestrator/runner/runner_test.go`

- [ ] **Step 1: Rewrite the choice tests (failing test)**

In `runner_test.go`, replace the three existing choice tests (`TestChoiceRunnerMatch`, `TestChoiceRunnerNoMatch`, and the numeric `TestChoiceRunnerNumericMatch`/`TestChoiceRunnerNumericNoMatch` added earlier) with string-expression versions. Keep `TestChoiceRunnerDefault` as-is.

```go
func TestChoiceRunnerMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":"input.has_tool_calls == true","Next":"tools"}],"Default":"end"}`),
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
		Config: json.RawMessage(`{"Choices":[{"Condition":"input.has_tool_calls == true","Next":"tools"}],"Default":"end"}`),
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

func TestChoiceRunnerNumericMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":"input.count == 3","Next":"hit"}],"Default":"end"}`),
	}
	input := map[string]interface{}{"count": 3} // Go int vs literal 3 (float64)
	result, err := r.Run(context.Background(), node, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "hit" {
		t.Fatalf("expected next 'hit', got %q", result.Next)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/runner/ -run TestChoiceRunner -v`
Expected: FAIL/build error — JSON `"Condition":"..."` (a string) cannot unmarshal into the current `json.RawMessage` field as a usable map, and `evaluateCondition` no longer matches; specifically `TestChoiceRunnerMatch` returns `"end"` instead of `"tools"`.

- [ ] **Step 3: Write minimal implementation**

Rewrite `pkg/orchestrator/runner/choice.go`:

```go
package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/condition"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// ChoiceConfig configures a choice (branch) node.
type ChoiceConfig struct {
	Choices []ChoiceEntry `json:"Choices"`
	Default string        `json:"Default,omitempty"`
}

// ChoiceEntry is a single branch. Condition is a string expression evaluated
// against the choice node's input; an empty Condition always matches.
type ChoiceEntry struct {
	Next      string `json:"Next"`
	Condition string `json:"Condition,omitempty"`
}

// Validate checks every choice condition at graph-load time.
func (c *ChoiceConfig) Validate() error {
	for _, ch := range c.Choices {
		if ch.Condition == "" {
			continue
		}
		if err := condition.Validate(ch.Condition); err != nil {
			return fmt.Errorf("choice -> %q: %w", ch.Next, err)
		}
	}
	return nil
}

// ChoiceRunner evaluates conditions and routes to the matching branch.
type ChoiceRunner struct{}

func (r *ChoiceRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg ChoiceConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ChoiceConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	scope := condition.Scope{Input: input}
	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.WorkMem != nil {
		scope.State = ec.WorkMem.State
	}

	for _, ch := range cfg.Choices {
		if ch.Condition == "" {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: ch.Next, Output: input}, nil
		}
		matched, err := condition.Evaluate(ch.Condition, scope)
		if err != nil {
			return nil, fmt.Errorf("choice condition %q: %w", ch.Condition, err)
		}
		if matched {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: ch.Next, Output: input}, nil
		}
	}

	if cfg.Default != "" {
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: cfg.Default, Output: input}, nil
	}

	return nil, fmt.Errorf("no choice matched and no default")
}

func init() {
	orchestrator.RegisterNodeType("choice", &ChoiceRunner{}, &ChoiceConfig{})
}
```

(This deletes the old `evaluateCondition`, `valuesEqual`, and `toFloat64` from `choice.go`; that numeric logic now lives in the `condition` package.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/runner/ -v`
Expected: PASS (all runner tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/runner/choice.go pkg/orchestrator/runner/runner_test.go
git commit -m "feat(condition): choice runner evaluates string conditions via condition pkg"
```

---

## Task 7: edge routing evaluates conditions in priority order

**Files:**
- Modify: `pkg/orchestrator/graph.go`
- Modify: `pkg/orchestrator/executor/route.go`
- Modify: `pkg/orchestrator/executor/route_test.go`

- [ ] **Step 1: Change EdgeSpec.Condition to string**

In `pkg/orchestrator/graph.go`, change the `EdgeSpec` field type (and drop the now-unused `encoding/json` import only if nothing else uses it — `NodeSpec.Config` still uses `json.RawMessage`, so keep the import):

```go
// EdgeSpec defines a directed edge between nodes. Condition is an optional
// string expression; an edge with no Condition is an unconditional fallback.
type EdgeSpec struct {
	From      string `json:"From"`
	To        string `json:"To"`
	Condition string `json:"Condition,omitempty"`
	Priority  int    `json:"Priority"`
	Label     string `json:"Label,omitempty"`
}
```

- [ ] **Step 2: Write the failing test**

In `route_test.go`, add tests for conditional routing. (Inspect the existing file first to reuse its helpers/imports; these tests assume `executor` is the package and import `orchestrator` + `agcontext`.)

```go
func TestRoute_ConditionalEdgeFirstMatchWins(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{"has_tool_calls": true},
	}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
		{From: "n", To: "end", Priority: 1}, // unconditional fallback
	}
	next, err := Route(context.Background(), "n", result, edges, ec)
	if err != nil {
		t.Fatal(err)
	}
	if next != "tools" {
		t.Fatalf("expected 'tools', got %q", next)
	}
}

func TestRoute_FallsThroughToUnconditional(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{"has_tool_calls": false},
	}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
		{From: "n", To: "end", Priority: 1},
	}
	next, err := Route(context.Background(), "n", result, edges, ec)
	if err != nil {
		t.Fatal(err)
	}
	if next != "end" {
		t.Fatalf("expected 'end', got %q", next)
	}
}

func TestRoute_NoMatchingConditionalEdgeErrors(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{"has_tool_calls": false},
	}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
	}
	if _, err := Route(context.Background(), "n", result, edges, ec); err == nil {
		t.Fatal("expected error when no conditional edge matches and no fallback")
	}
}

func TestRoute_DynamicNextOverridesEdges(t *testing.T) {
	ec := agcontext.NewExecutionContext(nil)
	result := &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: "explicit"}
	edges := []orchestrator.EdgeSpec{
		{From: "n", To: "tools", Condition: "input.has_tool_calls == true", Priority: 0},
	}
	next, err := Route(context.Background(), "n", result, edges, ec)
	if err != nil {
		t.Fatal(err)
	}
	if next != "explicit" {
		t.Fatalf("expected 'explicit', got %q", next)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/executor/ -run TestRoute_ -v`
Expected: FAIL — current `Route` ignores `Condition` and would return `"tools"` for the fall-through case (picks lowest Priority regardless of condition).

- [ ] **Step 4: Write minimal implementation**

Rewrite `pkg/orchestrator/executor/route.go`:

```go
package executor

import (
	"context"
	"fmt"
	"sort"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/condition"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// Route determines the next node from the current node's result and graph edges.
// Priority: result.Next (dynamic) > first outgoing edge (in Priority order) whose
// Condition passes > error. An edge with no Condition is an unconditional fallback
// and always passes.
func Route(ctx context.Context, currentNode string, result *orchestrator.NodeResult,
	edges []orchestrator.EdgeSpec, ec interface{}) (string, error) {

	// Dynamic override from the node result.
	if result.Next != "" {
		return result.Next, nil
	}

	// Outgoing edges sorted by priority (lower = higher priority).
	var outgoing []orchestrator.EdgeSpec
	for _, e := range edges {
		if e.From == currentNode {
			outgoing = append(outgoing, e)
		}
	}
	sort.SliceStable(outgoing, func(i, j int) bool {
		return outgoing[i].Priority < outgoing[j].Priority
	})

	scope := condition.Scope{Input: result.Output}
	if ac, ok := ec.(*agcontext.ExecutionContext); ok && ac.WorkMem != nil {
		scope.State = ac.WorkMem.State
	}

	for _, e := range outgoing {
		if e.Condition == "" {
			return e.To, nil
		}
		matched, err := condition.Evaluate(e.Condition, scope)
		if err != nil {
			return "", fmt.Errorf("edge %q->%q condition %q: %w", currentNode, e.To, e.Condition, err)
		}
		if matched {
			return e.To, nil
		}
	}

	return "", fmt.Errorf("no matching edge from node %q (no condition passed and no fallback)", currentNode)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/executor/ -v`
Expected: PASS. If any pre-existing route test assumed pure-priority behavior with edges that have no conditions, it still passes (unconditional edges return immediately in priority order).

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/graph.go pkg/orchestrator/executor/route.go pkg/orchestrator/executor/route_test.go
git commit -m "feat(condition): edge routing evaluates conditions in priority order"
```

---

## Task 8: validate conditions at graph-load time

**Files:**
- Modify: `pkg/orchestrator/graph_json.go`
- Test: `pkg/orchestrator/graph_json_test.go`

- [ ] **Step 1: Write the failing test**

Add to `graph_json_test.go` (reuse the file's existing package + imports):

```go
func TestUnmarshalGraph_RejectsBadEdgeCondition(t *testing.T) {
	data := []byte(`{
		"StartAt": "a",
		"Nodes": {"a": {"Type": "end"}, "b": {"Type": "end"}},
		"Edges": [{"From": "a", "To": "b", "Condition": "input.x + 1"}]
	}`)
	if _, err := UnmarshalGraph(data); err == nil {
		t.Fatal("expected error for invalid edge condition, got nil")
	}
}

func TestUnmarshalGraph_RejectsBadChoiceCondition(t *testing.T) {
	data := []byte(`{
		"StartAt": "c",
		"Nodes": {
			"c": {"Type": "choice", "Config": {"Choices": [{"Condition": "foo(bar)", "Next": "end"}], "Default": "end"}},
			"end": {"Type": "end"}
		}
	}`)
	if _, err := UnmarshalGraph(data); err == nil {
		t.Fatal("expected error for invalid choice condition, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/orchestrator/ -run TestUnmarshalGraph_Rejects -v`
Expected: FAIL — `UnmarshalGraph` currently performs no condition validation, so both return nil error.

- [ ] **Step 3: Write minimal implementation**

In `pkg/orchestrator/graph_json.go`, add `"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/condition"` to imports. After the node loop and before `return g, nil`, validate edge conditions:

```go
	for i, e := range g.Edges {
		if e.Condition == "" {
			continue
		}
		if err := condition.Validate(e.Condition); err != nil {
			return nil, fmt.Errorf("edge %d (%s->%s): %w", i, e.From, e.To, err)
		}
	}

	return g, nil
}
```

Then, inside `unmarshalNode`, after `node.ParsedConfig = cfgPtr` is set, invoke the optional validation hook so choice (and any future) node configs validate their own conditions:

```go
		node.ParsedConfig = cfgPtr

		if v, ok := cfgPtr.(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return nil, fmt.Errorf("validate config for type %q: %w", typeCheck.Type, err)
			}
		}
	}

	return &node, nil
}
```

(`runner.ChoiceConfig.Validate()` was added in Task 6, so the hook picks it up. No import of `runner` from `orchestrator` is needed — the hook is a structural interface.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./pkg/orchestrator/ -v`
Expected: PASS, including the two new rejection tests and all existing graph tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/graph_json.go pkg/orchestrator/graph_json_test.go
git commit -m "feat(condition): validate node and edge conditions at graph-load time"
```

---

## Task 9: migrate the default graph and verify the whole suite

**Files:**
- Modify: `pkg/agent/graph_builder.go`
- Modify (if needed): `pkg/agent/agent_test.go`

- [ ] **Step 1: Update the default graph to string conditions**

In `pkg/agent/graph_builder.go`, change the `route` node's choices in `defaultGraphJSON`:

```json
    "route": {
      "Type": "choice",
      "Config": {
        "Choices": [
          {"Condition": "input.has_tool_calls == true", "Next": "dispatch_tools"},
          {"Condition": "input.needs_compression == true", "Next": "compress"}
        ],
        "Default": "end"
      }
    },
```

- [ ] **Step 2: Run the default-graph build test to verify it still parses**

Run: `go test ./pkg/agent/ -run TestBuildDefaultGraph_NoError -v`
Expected: PASS — the graph now also passes load-time condition validation. If it fails on validation, the expression syntax in Step 1 is wrong; fix it.

- [ ] **Step 3: Check agent_test.go for stale condition references**

Inspect `pkg/agent/agent_test.go` around the `has_tool_calls` usage (line ~71). If a test constructs a choice config or graph JSON using the old map-equality `Condition` form, rewrite it to the string form (e.g. `"input.has_tool_calls == true"`). If it only sets `has_tool_calls` as input data (not as a condition), leave it unchanged.

Run: `go test ./pkg/agent/ -v`
Expected: PASS. Fix any test that still uses object-form conditions.

- [ ] **Step 4: Full verification**

Run each and confirm:
- `gofmt -l pkg/orchestrator/condition pkg/orchestrator/runner pkg/orchestrator/executor pkg/orchestrator pkg/agent` → no output
- `go vet ./...` → no errors
- `go build ./...` → succeeds
- `go test ./...` → all packages `ok`, no `FAIL`

- [ ] **Step 5: Commit**

```bash
git add pkg/agent/graph_builder.go pkg/agent/agent_test.go
git commit -m "feat(condition): migrate default graph to string-expression conditions"
```

---

## Self-Review Notes

- **Spec coverage:** Evaluator interface + Default (Task 1) ✓; input/state scope (Task 2) ✓; comparisons w/ numeric normalization incl. the float64 bug (Task 3) ✓; logical ops (Task 4) ✓; Validate (Task 5) ✓; clean switch of choice to string + removal of old matcher (Task 6) ✓; edge routing semantics result.Next > priority-ordered first-match > fallback (Task 7) ✓; load-time validation for edges + choice via hook (Task 8) ✓; default graph migration + clean switch verification (Task 9) ✓.
- **Type consistency:** `Scope{Input, State}`, `Evaluator{Evaluate, Validate}`, `condition.Evaluate`/`condition.Validate`, `ChoiceEntry.Condition string`, `EdgeSpec.Condition string`, `toFloat64` (in condition pkg) — names consistent across tasks.
- **Known limitation (documented):** dot-access field keys must be valid Go identifiers; arbitrary keys (hyphens) are out of scope. Membership (`in`), arithmetic, string functions, and conversation/scratchpad scope are intentionally deferred (YAGNI).
