package adapters

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The skills tool's activate/deactivate/read need per-agent state, so Invoke
// must route "skills" to the injected SkillsFn rather than the registry's
// list-only fallback. Regression guard for the graph-migration bug where
// handleSkillsCall was never called.
func TestInvoke_SkillsRoutesToHandler(t *testing.T) {
	var gotRaw json.RawMessage
	a := &RegistryAdapter{
		SkillsFn: func(raw json.RawMessage) string {
			gotRaw = raw
			return "handled"
		},
	}

	res, err := a.Invoke(context.Background(), "skills",
		map[string]interface{}{"action": "activate", "name": "foo"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := res.Output.(string); out != "handled" {
		t.Fatalf("skills not routed to SkillsFn, got %v", res.Output)
	}
	if !strings.Contains(string(gotRaw), "activate") || !strings.Contains(string(gotRaw), "foo") {
		t.Fatalf("args not passed through to handler: %s", gotRaw)
	}
}
