package grapheditor

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // expected extracted JSON, "" means ok=false
	}{
		{"bare", `{"StartAt":"a","Nodes":{},"Edges":[]}`, `{"StartAt":"a","Nodes":{},"Edges":[]}`},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"fenced-no-lang", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"prose-around", "好的,这是图:\n{\"a\":1}\n以上。", `{"a":1}`},
		{"none", "我无法生成。", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := extractJSON(c.in)
			if c.want == "" {
				if ok {
					t.Fatalf("expected ok=false, got %q", string(got))
				}
				return
			}
			if !ok {
				t.Fatalf("expected ok=true for %q", c.in)
			}
			if string(got) != c.want {
				t.Fatalf("got %q, want %q", string(got), c.want)
			}
		})
	}
}
