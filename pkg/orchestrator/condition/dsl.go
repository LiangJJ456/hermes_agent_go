package condition

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
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
	case *ast.BinaryExpr:
		return evalBinary(node, scope)
	case *ast.BasicLit:
		return evalLit(node)
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

// checkNode is filled in by Task 5. For now accept any node so Validate is a
// no-op beyond parsing; later tasks tighten it.
func checkNode(n ast.Expr) error { return nil }
