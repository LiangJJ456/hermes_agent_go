package mempalace

import "testing"

func TestFilterRelevantKeepsQueryHits(t *testing.T) {
	results := []SearchResult{
		{Score: 0.9, VectorScore: 0.5, BM25Score: 0},   // 向量命中 → 保留
		{Score: 0.8, VectorScore: 0, BM25Score: 1.2},   // 关键词命中 → 保留
		{Score: 0.7, VectorScore: 0.1, BM25Score: 0},   // 仅 importance、query 零命中 → 剔除
	}
	kept := filterRelevant(results, 0.35, 0.0, 3)
	if len(kept) != 2 {
		t.Fatalf("expected 2 kept, got %d", len(kept))
	}
}

func TestFilterRelevantDropsImportanceOnly(t *testing.T) {
	results := []SearchResult{
		{VectorScore: 0.1, BM25Score: 0},
		{VectorScore: 0.2, BM25Score: 0},
	}
	if got := filterRelevant(results, 0.35, 0.0, 3); len(got) != 0 {
		t.Fatalf("expected 0 kept, got %d", len(got))
	}
}

func TestFilterRelevantCapsAtLimit(t *testing.T) {
	var results []SearchResult
	for i := 0; i < 6; i++ {
		results = append(results, SearchResult{VectorScore: 0.9, BM25Score: 1})
	}
	if got := filterRelevant(results, 0.35, 0.0, 3); len(got) != 3 {
		t.Fatalf("expected cap 3, got %d", len(got))
	}
}
