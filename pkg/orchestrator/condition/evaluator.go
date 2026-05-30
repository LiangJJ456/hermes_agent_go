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
