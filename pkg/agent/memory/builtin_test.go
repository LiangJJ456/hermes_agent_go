package memory

import (
	"context"
	"testing"
)

func TestBuiltinProviderPrefetchReturnsEmpty(t *testing.T) {
	store := NewStore(t.TempDir(), 0, 0)
	p := NewBuiltinProvider(store)

	// 即使存有内容,builtin 也不应通过预取注入(它只走 SystemPromptBlock)
	p.store.Add("memory", "some durable note")

	if got := p.Prefetch(context.Background(), "note", "sess"); got != "" {
		t.Fatalf("expected empty prefetch, got %q", got)
	}
}
