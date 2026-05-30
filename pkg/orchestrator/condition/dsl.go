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
		if v == undefined {
			return false, nil
		}
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
