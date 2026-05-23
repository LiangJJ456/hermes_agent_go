package credential

import (
	"testing"
	"time"
)

func TestPool_Acquire_RoundRobin(t *testing.T) {
	pool := NewPool(StrategyRoundRobin)
	pool.Add(&Credential{ID: "a", APIKey: "key_a"})
	pool.Add(&Credential{ID: "b", APIKey: "key_b"})
	pool.Add(&Credential{ID: "c", APIKey: "key_c"})

	// Round-robin should cycle through
	seen := make(map[string]int)
	for i := 0; i < 9; i++ {
		cred := pool.Acquire()
		if cred == nil {
			t.Fatal("Acquire returned nil")
		}
		seen[cred.ID]++
	}

	for _, id := range []string{"a", "b", "c"} {
		if seen[id] != 3 {
			t.Errorf("expected credential %s to be used 3 times, got %d", id, seen[id])
		}
	}
}

func TestPool_Acquire_Cooldown(t *testing.T) {
	pool := NewPool(StrategyRoundRobin)
	pool.Add(&Credential{ID: "a", APIKey: "key_a"})
	pool.Add(&Credential{ID: "b", APIKey: "key_b"})

	// Cooldown "a"
	pool.HandleError("a", 429)

	// Should only get "b" now
	for i := 0; i < 5; i++ {
		cred := pool.Acquire()
		if cred == nil {
			t.Fatal("Acquire returned nil")
		}
		if cred.ID == "a" {
			t.Error("credential 'a' should be in cooldown")
		}
	}
}

func TestPool_Acquire_AllCooledDown(t *testing.T) {
	pool := NewPool(StrategyRoundRobin)
	pool.Add(&Credential{ID: "a", APIKey: "key_a"})

	// Cooldown the only credential
	pool.HandleError("a", 429)

	cred := pool.Acquire()
	if cred != nil {
		t.Error("expected nil when all credentials are cooled down")
	}
}

func TestCredential_IsAvailable(t *testing.T) {
	cred := &Credential{ID: "test"}

	if !cred.IsAvailable() {
		t.Error("new credential should be available")
	}

	cred.Cooldown(100*time.Millisecond, "test")
	if cred.IsAvailable() {
		t.Error("credential should be cooling down")
	}

	time.Sleep(150 * time.Millisecond)
	if !cred.IsAvailable() {
		t.Error("credential should be available after cooldown")
	}
}

func TestPool_AvailableCount(t *testing.T) {
	pool := NewPool(StrategyRandom)
	pool.Add(&Credential{ID: "a", APIKey: "key_a"})
	pool.Add(&Credential{ID: "b", APIKey: "key_b"})

	if pool.AvailableCount() != 2 {
		t.Errorf("expected 2, got %d", pool.AvailableCount())
	}

	pool.HandleError("a", 402)
	if pool.AvailableCount() != 1 {
		t.Errorf("expected 1 after cooldown, got %d", pool.AvailableCount())
	}
}
