package condition

import "testing"

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
		{"input.missing", false}, // missing key -> undefined -> bare bool is false
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

func TestEvaluate_Comparisons(t *testing.T) {
	scope := Scope{
		Input: map[string]interface{}{
			"count": 3, // Go int (runtime value)
			"name":  "alice",
			"flag":  true,
		},
	}
	cases := []struct {
		expr string
		want bool
	}{
		{"input.count == 3", true}, // int vs JSON-number(float64) must match
		{"input.count != 3", false},
		{"input.count > 2", true},
		{"input.count < 2", false},
		{"input.count >= 3", true},
		{"input.count <= 2", false},
		{`input.name == "alice"`, true},
		{`input.name != "bob"`, true},
		{"input.flag == true", true},
		{"input.flag == false", false},
		{"input.missing == 3", false}, // undefined vs value -> false
		{"input.missing != 3", true},  // undefined != value -> true
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
