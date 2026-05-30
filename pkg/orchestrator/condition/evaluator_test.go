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
