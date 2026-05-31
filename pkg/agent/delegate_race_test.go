package agent

import (
	"sync"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// TestConcurrentChildAgentsNoRace creates many child agents concurrently. With
// the old global-singleton runners this raced on shared mutable fields; with
// per-execution services there is no shared mutable state. Run with -race.
func TestConcurrentChildAgentsNoRace(t *testing.T) {
	parent, err := NewAIAgent(types.AgentConfig{MaxDelegateDepth: 2}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parent.SetEventCallback(func(Event) {})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			child, err := parent.NewChildAgent("task")
			if err != nil {
				t.Error(err)
				return
			}
			_ = child.executor
		}()
	}
	wg.Wait()
}
