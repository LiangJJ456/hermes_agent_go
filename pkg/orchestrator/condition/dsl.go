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
