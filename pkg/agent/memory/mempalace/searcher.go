package mempalace

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
)

// ---------------------------------------------------------------------------
// SearchResult — single hit returned to callers
// ---------------------------------------------------------------------------

// SearchResult represents a single search hit with hybrid scoring.
type SearchResult struct {
	Drawer      *Drawer `json:"drawer"`
	Score       float64 `json:"score"`        // final hybrid score
	VectorScore float64 `json:"vector_score"` // cosine similarity (1 - distance)
	BM25Score   float64 `json:"bm25_score"`   // raw Okapi-BM25 score
	MatchType   string  `json:"match_type"`   // "vector", "keyword", "hybrid"
}

// ---------------------------------------------------------------------------
// Searcher — hybrid BM25 + ChromaDB vector search
// ---------------------------------------------------------------------------

const (
	defaultVectorWeight = 0.6
	defaultBM25Weight   = 0.4
	chromaCollName      = "mempalace_drawers"
	// ChromaDB cosine distance space: 0 = identical, 2 = opposite.
	// We convert to similarity: max(0, 1 - distance).
)

// Searcher provides hybrid BM25 + ChromaDB vector semantic search over drawers.
// Inspired by mempalace's searcher.py: vector retrieval is the floor (always
// runs), BM25 re-ranks candidates, closet/entity boosts add signals.
type Searcher struct {
	store *DrawerStore

	mu       sync.RWMutex
	chromaOK bool // true after successful InitChroma
	client   chroma.Client
	col      chroma.Collection

	vectorWeight float64
	bm25Weight   float64
}

// NewSearcher creates a searcher backed by the given drawer store.
// Call InitChroma() after construction to enable vector search.
// Without ChromaDB the searcher falls back to pure BM25.
func NewSearcher(store *DrawerStore) *Searcher {
	return &Searcher{
		store:        store,
		vectorWeight: defaultVectorWeight,
		bm25Weight:   defaultBM25Weight,
	}
}

// ---------------------------------------------------------------------------
// ChromaDB lifecycle
// ---------------------------------------------------------------------------

// InitChroma initialises the ChromaDB persistent client and collection.
// palacePath is the root palace directory; ChromaDB data lives under
// {palacePath}/chroma.
//
// If ChromaDB is unavailable the searcher silently degrades to pure BM25.
func (s *Searcher) InitChroma(palacePath string) error {
	ctx := context.Background()

	chromaPath := palacePath + "/chroma"
	client, err := chroma.NewPersistentClient(
		chroma.WithPersistentPath(chromaPath),
	)
	if err != nil {
		return fmt.Errorf("chroma persistent client: %w", err)
	}

	col, err := client.GetOrCreateCollection(ctx, chromaCollName)
	if err != nil {
		return fmt.Errorf("chroma collection: %w", err)
	}

	s.mu.Lock()
	s.client = client
	s.col = col
	s.chromaOK = true
	s.mu.Unlock()

	return nil
}

// IsChromaReady returns true if ChromaDB is initialized and usable.
func (s *Searcher) IsChromaReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chromaOK
}

// IndexDrawer upserts a drawer into the ChromaDB collection.
// Metadata includes wing, room, importance, entities (comma-joined),
// source, and filed_at for filtering and ranking.
func (s *Searcher) IndexDrawer(d *Drawer) error {
	s.mu.RLock()
	ok := s.chromaOK
	col := s.col
	s.mu.RUnlock()
	if !ok || col == nil {
		return nil // silently skip when ChromaDB not available
	}

	ctx := context.Background()
	meta := map[string]interface{}{
		"wing":       d.Wing,
		"room":       d.Room,
		"importance": int(d.Importance),
		"source":     d.Source,
		"filed_at":   d.FiledAt.Format("2006-01-02T15:04:05Z"),
	}
	if len(d.Entities) > 0 {
		meta["entities"] = strings.Join(d.Entities, ",")
	}

	chromaMeta, err := chroma.NewDocumentMetadataFromMap(meta)
	if err != nil {
		return fmt.Errorf("chroma metadata: %w", err)
	}

	return col.Add(ctx,
		chroma.WithIDs(chroma.DocumentID(d.ID)),
		chroma.WithTexts(d.Content),
		chroma.WithMetadatas(chromaMeta),
	)
}

// RemoveDrawer deletes a drawer from the ChromaDB collection by ID.
func (s *Searcher) RemoveDrawer(id string) error {
	s.mu.RLock()
	ok := s.chromaOK
	col := s.col
	s.mu.RUnlock()
	if !ok || col == nil {
		return nil
	}

	ctx := context.Background()
	return col.Delete(ctx, chroma.WithIDs(chroma.DocumentID(id)))
}

// ReindexAll bulk-indexes all drawers from the store into ChromaDB.
// Useful after first InitChroma or to rebuild the index.
func (s *Searcher) ReindexAll() error {
	s.mu.RLock()
	ok := s.chromaOK
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("ChromaDB not initialized")
	}

	drawers := s.store.All()
	for _, d := range drawers {
		if err := s.IndexDrawer(d); err != nil {
			return fmt.Errorf("reindex drawer %s: %w", d.ID, err)
		}
	}
	return nil
}

// CloseChroma releases the ChromaDB client resources.
func (s *Searcher) CloseChroma() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chromaOK = false
	s.client = nil
	s.col = nil
}

// ---------------------------------------------------------------------------
// Hybrid Search — the main entry point
// ---------------------------------------------------------------------------

// Search performs hybrid BM25 + vector semantic search.
//
// When ChromaDB is available:
//  1. Query ChromaDB for n_results * 3 candidates (over-fetch for re-ranking)
//  2. Run BM25 on the candidate documents
//  3. Hybrid rank: vector_weight * cosine_sim + bm25_weight * bm25_norm
//  4. Apply entity boost and importance weight
//
// When ChromaDB is NOT available:
//
//	Falls back to pure BM25 over all drawers (original behavior).
func (s *Searcher) Search(query string, wing, room string, nResults int) []SearchResult {
	if nResults <= 0 {
		nResults = 5
	}

	s.mu.RLock()
	chromaReady := s.chromaOK
	col := s.col
	s.mu.RUnlock()

	if chromaReady && col != nil {
		results := s.hybridSearch(col, query, wing, room, nResults)
		if results != nil {
			return results
		}
		// If hybrid search fails, fall through to BM25 fallback
	}

	return s.bm25Fallback(query, wing, room, nResults)
}

// ---------------------------------------------------------------------------
// Hybrid search (ChromaDB + BM25)
// ---------------------------------------------------------------------------

func (s *Searcher) hybridSearch(col chroma.Collection, query, wing, room string, nResults int) []SearchResult {
	ctx := context.Background()

	// Over-fetch for re-ranking (mempalace uses n_results * 3)
	overFetch := nResults * 3
	if overFetch < 10 {
		overFetch = 10
	}

	// Build query with wing/room filter
	whereFilter := buildWhereFilter(wing, room)

	var qr chroma.QueryResult
	var err error
	if whereFilter != nil {
		qr, err = col.Query(ctx,
			chroma.WithQueryTexts(query),
			chroma.WithNResults(overFetch),
			chroma.WithInclude(chroma.IncludeDocuments, chroma.IncludeMetadatas, chroma.IncludeDistances),
			chroma.WithWhere(whereFilter),
		)
	} else {
		qr, err = col.Query(ctx,
			chroma.WithQueryTexts(query),
			chroma.WithNResults(overFetch),
			chroma.WithInclude(chroma.IncludeDocuments, chroma.IncludeMetadatas, chroma.IncludeDistances),
		)
	}
	if err != nil || qr == nil {
		return nil // fall back to BM25
	}

	// Extract results from ChromaDB response using accessor methods.
	idGrps := qr.GetIDGroups()
	if len(idGrps) == 0 || len(idGrps[0]) == 0 {
		return nil
	}
	ids := idGrps[0]

	var docs []string
	docGroups := qr.GetDocumentsGroups()
	if len(docGroups) > 0 {
		for _, doc := range docGroups[0] {
			docs = append(docs, doc.ContentString())
		}
	}

	var dists []float64
	distGroups := qr.GetDistancesGroups()
	if len(distGroups) > 0 {
		for _, d := range distGroups[0] {
			dists = append(dists, float64(d))
		}
	}

	var metas []map[string]interface{}
	metaGroups := qr.GetMetadatasGroups()
	if len(metaGroups) > 0 {
		for _, m := range metaGroups[0] {
			mm := make(map[string]interface{})
			if m != nil {
				// Convert Metadata to map via JSON round-trip
				if raw, err := json.Marshal(m); err == nil {
					_ = json.Unmarshal(raw, &mm)
				}
			}
			metas = append(metas, mm)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	// Build candidate list with vector scores
	type candidate struct {
		id       chroma.DocumentID
		text     string
		distance float64
		vecSim   float64 // max(0, 1 - distance)
		meta     map[string]interface{}
		drawer   *Drawer
	}

	var candidates []candidate
	for i, id := range ids {
		text := ""
		if i < len(docs) {
			text = docs[i]
		}
		dist := 1.0
		if i < len(dists) {
			dist = dists[i]
		}
		var meta map[string]interface{}
		if i < len(metas) {
			meta = metas[i]
		}

		vecSim := math.Max(0.0, 1.0-dist)

		// Resolve drawer from store (for full metadata)
		drawer, ok := s.store.Get(string(id))
		if !ok {
			// Drawer exists in ChromaDB but not in store — create a minimal one
			w := stringFromMeta(meta, "wing")
			r := stringFromMeta(meta, "room")
			drawer = &Drawer{
				ID:      string(id),
				Content: text,
				Wing:    w,
				Room:    r,
			}
		}

		candidates = append(candidates, candidate{
			id:       id,
			text:     text,
			distance: dist,
			vecSim:   vecSim,
			meta:     meta,
			drawer:   drawer,
		})
	}

	// BM25 re-ranking over the candidate documents
	candidateDocs := make([]string, len(candidates))
	for i, c := range candidates {
		// Combine content + entities for richer BM25 matching
		candidateDocs[i] = c.text + " " + strings.Join(c.drawer.Entities, " ")
	}
	bm25Scores := bm25(query, candidateDocs, 1.5, 0.75)

	// Normalize BM25 scores (min-max normalization, matching mempalace)
	maxBM25 := 0.0
	for _, s := range bm25Scores {
		if s > maxBM25 {
			maxBM25 = s
		}
	}
	bm25Norm := make([]float64, len(bm25Scores))
	if maxBM25 > 0 {
		for i, s := range bm25Scores {
			bm25Norm[i] = s / maxBM25
		}
	}

	// Hybrid ranking: vector_weight * vec_sim + bm25_weight * bm25_norm
	queryLower := strings.ToLower(query)
	queryTokens := tokenize(queryLower)

	var results []SearchResult
	for i, c := range candidates {
		hybridScore := s.vectorWeight*c.vecSim + s.bm25Weight*bm25Norm[i]

		// Entity matching boost (same as mempalace)
		entityBoost := 0.0
		for _, ent := range c.drawer.Entities {
			if strings.Contains(queryLower, strings.ToLower(ent)) {
				entityBoost += 0.3
			}
		}

		// Wing/room name matching boost
		if wing == "" && containsAny(queryTokens, tokenize(c.drawer.Wing)) {
			hybridScore += 0.05
		}
		if room == "" && containsAny(queryTokens, tokenize(c.drawer.Room)) {
			hybridScore += 0.05
		}

		// Importance weight: importance / 10.0
		importanceBoost := c.drawer.Importance / 10.0

		finalScore := hybridScore + entityBoost + importanceBoost

		matchType := "vector"
		if bm25Scores[i] > 0.01 && c.vecSim > 0.01 {
			matchType = "hybrid"
		} else if bm25Scores[i] > 0.01 {
			matchType = "keyword"
		}

		results = append(results, SearchResult{
			Drawer:      c.drawer,
			Score:       finalScore,
			VectorScore: c.vecSim,
			BM25Score:   bm25Scores[i],
			MatchType:   matchType,
		})
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if nResults > len(results) {
		nResults = len(results)
	}
	return results[:nResults]
}

// ---------------------------------------------------------------------------
// BM25 fallback (no ChromaDB)
// ---------------------------------------------------------------------------

func (s *Searcher) bm25Fallback(query, wing, room string, nResults int) []SearchResult {
	drawers := s.store.All()
	if len(drawers) == 0 {
		return nil
	}

	// Filter by wing/room
	var candidates []*Drawer
	for _, d := range drawers {
		if wing != "" && d.Wing != wing {
			continue
		}
		if room != "" && d.Room != room {
			continue
		}
		candidates = append(candidates, d)
	}
	if len(candidates) == 0 {
		return nil
	}

	// Build document corpus for BM25
	docs := make([]string, len(candidates))
	for i, d := range candidates {
		docs[i] = d.Content + " " + strings.Join(d.Entities, " ")
	}

	bm25Scores := bm25(query, docs, 1.5, 0.75)

	queryLower := strings.ToLower(query)
	queryTokens := tokenize(queryLower)

	var results []SearchResult
	for i, d := range candidates {
		score := bm25Scores[i]

		// Entity matching boost
		entityBoost := 0.0
		for _, ent := range d.Entities {
			if strings.Contains(queryLower, strings.ToLower(ent)) {
				entityBoost += 0.3
			}
		}

		// Room/wing name matching boost
		if wing == "" && containsAny(queryTokens, tokenize(d.Wing)) {
			score += 0.1
		}
		if room == "" && containsAny(queryTokens, tokenize(d.Room)) {
			score += 0.1
		}

		// Importance weight
		importanceBoost := d.Importance / 10.0

		finalScore := score + entityBoost + importanceBoost

		if finalScore > 0.01 {
			matchType := "keyword"
			if entityBoost > 0 && score > 0 {
				matchType = "hybrid"
			} else if entityBoost > 0 {
				matchType = "entity"
			}

			results = append(results, SearchResult{
				Drawer:      d,
				Score:       finalScore,
				VectorScore: 0, // no vector search in fallback
				BM25Score:   score,
				MatchType:   matchType,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if nResults > len(results) {
		nResults = len(results)
	}
	return results[:nResults]
}

// ---------------------------------------------------------------------------
// ChromaDB where-filter builder (uses typed filter API)
// ---------------------------------------------------------------------------

func buildWhereFilter(wing, room string) chroma.WhereClause {
	if wing != "" && room != "" {
		return chroma.And(
			chroma.EqString("wing", wing),
			chroma.EqString("room", room),
		)
	}
	if wing != "" {
		return chroma.EqString("wing", wing)
	}
	if room != "" {
		return chroma.EqString("room", room)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Metadata helpers
// ---------------------------------------------------------------------------

func stringFromMeta(meta map[string]interface{}, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// BM25 Implementation — Okapi BM25 with Lucene-style IDF smoothing
// Matches mempalace's searcher.py _bm25_scores()
// ---------------------------------------------------------------------------

var tokenRe = regexp.MustCompile(`\w{2,}`)

func tokenize(text string) []string {
	return tokenRe.FindAllString(strings.ToLower(text), -1)
}

func bm25(query string, documents []string, k1, b float64) []float64 {
	nDocs := len(documents)
	queryTerms := uniqueSet(tokenize(query))
	if len(queryTerms) == 0 || nDocs == 0 {
		return make([]float64, nDocs)
	}

	// Tokenize all documents
	tokenized := make([][]string, nDocs)
	docLens := make([]int, nDocs)
	totalLen := 0
	for i, doc := range documents {
		tokens := tokenize(doc)
		tokenized[i] = tokens
		docLens[i] = len(tokens)
		totalLen += len(tokens)
	}

	avgDL := float64(totalLen) / float64(nDocs)
	if avgDL == 0 {
		avgDL = 1
	}

	// Document frequency
	df := make(map[string]int)
	for _, tokens := range tokenized {
		seen := make(map[string]bool)
		for _, tok := range tokens {
			if queryTerms[tok] && !seen[tok] {
				df[tok]++
				seen[tok] = true
			}
		}
	}

	// IDF: log((N - df + 0.5) / (df + 0.5) + 1)
	idf := make(map[string]float64)
	for term := range queryTerms {
		d := float64(df[term])
		n := float64(nDocs)
		idf[term] = math.Log((n-d+0.5)/(d+0.5) + 1)
	}

	// Score each document
	scores := make([]float64, nDocs)
	for i, tokens := range tokenized {
		dl := float64(docLens[i])
		if dl == 0 {
			continue
		}

		tf := make(map[string]int)
		for _, tok := range tokens {
			if queryTerms[tok] {
				tf[tok]++
			}
		}

		for term, freq := range tf {
			f := float64(freq)
			num := f * (k1 + 1)
			den := f + k1*(1-b+b*dl/avgDL)
			scores[i] += idf[term] * num / den
		}
	}

	return scores
}

func uniqueSet(tokens []string) map[string]bool {
	set := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		set[t] = true
	}
	return set
}

func containsAny(haystack, needles []string) bool {
	set := make(map[string]bool, len(haystack))
	for _, h := range haystack {
		set[h] = true
	}
	for _, n := range needles {
		if set[n] {
			return true
		}
	}
	return false
}
