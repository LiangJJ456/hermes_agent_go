package trace

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func BenchmarkNewTraceID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewTraceID()
	}
}

func BenchmarkNewSpanID(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewSpanID()
	}
}

// BenchmarkCryptoRandBaseline 对比基线：纯 crypto/rand
func BenchmarkCryptoRandBaseline(b *testing.B) {
	b.ReportAllocs()
	buf := make([]byte, 16)
	for i := 0; i < b.N; i++ {
		_, _ = rand.Read(buf)
		_ = hex.EncodeToString(buf)
	}
}

func TestTraceID_Uniqueness(t *testing.T) {
	const n = 100000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewTraceID()
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate TraceID at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestSpanID_Uniqueness(t *testing.T) {
	const n = 100000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := NewSpanID()
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate SpanID at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestTraceID_Length(t *testing.T) {
	id := NewTraceID()
	if len(id) != 32 { // 16 bytes = 32 hex chars
		t.Fatalf("expected 32 chars, got %d: %s", len(id), id)
	}
}

func TestSpanID_Length(t *testing.T) {
	id := NewSpanID()
	if len(id) != 16 { // 8 bytes = 16 hex chars
		t.Fatalf("expected 16 chars, got %d: %s", len(id), id)
	}
}
