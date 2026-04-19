package mempalace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// -- Layer 0: Identity --
// ~100 tokens. Always loaded. Read from identity.txt.

// Layer0 provides the agent's identity context.
type Layer0 struct {
	identityPath string
	cached       string
}

// NewLayer0 creates L0 reading from the given path.
func NewLayer0(palacePath string) *Layer0 {
	return &Layer0{
		identityPath: filepath.Join(palacePath, "identity.txt"),
	}
}

// Render returns the identity text.
func (l *Layer0) Render() string {
	if l.cached != "" {
		return l.cached
	}
	data, err := os.ReadFile(l.identityPath)
	if err != nil {
		l.cached = "## L0 — IDENTITY\nNo identity configured. Create identity.txt in the palace directory."
		return l.cached
	}
	l.cached = strings.TrimSpace(string(data))
	return l.cached
}

// TokenEstimate returns approximate token count.
func (l *Layer0) TokenEstimate() int {
	return len(l.Render()) / 4
}

// -- Layer 1: Essential Story --
// ~500-800 tokens. Always loaded. Auto-generated from top drawers.

const (
	l1MaxDrawers = 15
	l1MaxChars   = 3200
)

// Layer1 auto-generates a compact summary from the highest-importance drawers.
type Layer1 struct {
	store *DrawerStore
}

// NewLayer1 creates L1 from the given drawer store.
func NewLayer1(store *DrawerStore) *Layer1 {
	return &Layer1{store: store}
}

// Generate builds compact L1 text grouped by room.
func (l *Layer1) Generate(wing string) string {
	top := l.store.TopDrawers(l1MaxDrawers, wing)
	if len(top) == 0 {
		return "## L1 — No memories yet."
	}

	// Group by room
	byRoom := make(map[string][]*Drawer)
	for _, d := range top {
		byRoom[d.Room] = append(byRoom[d.Room], d)
	}

	// Sort room names
	rooms := make([]string, 0, len(byRoom))
	for r := range byRoom {
		rooms = append(rooms, r)
	}
	sort.Strings(rooms)

	var lines []string
	lines = append(lines, "## L1 — ESSENTIAL STORY")

	totalLen := 0
	for _, room := range rooms {
		roomLine := fmt.Sprintf("\n[%s]", room)
		lines = append(lines, roomLine)
		totalLen += len(roomLine)

		for _, d := range byRoom[room] {
			snippet := strings.ReplaceAll(strings.TrimSpace(d.Content), "\n", " ")
			if len(snippet) > 200 {
				snippet = snippet[:197] + "..."
			}

			entryLine := "  - " + snippet
			if d.Source != "" {
				entryLine += fmt.Sprintf("  (%s)", d.Source)
			}

			if totalLen+len(entryLine) > l1MaxChars {
				lines = append(lines, "  ... (more in L3 search)")
				return strings.Join(lines, "\n")
			}

			lines = append(lines, entryLine)
			totalLen += len(entryLine)
		}
	}

	return strings.Join(lines, "\n")
}

// -- Layer 2: On-Demand --
// ~200-500 tokens per retrieval. Loaded when a topic comes up.

// Layer2 provides filtered retrieval by wing/room.
type Layer2 struct {
	store *DrawerStore
}

// NewLayer2 creates L2 retrieval.
func NewLayer2(store *DrawerStore) *Layer2 {
	return &Layer2{store: store}
}

// Retrieve returns drawers filtered by wing and/or room.
func (l *Layer2) Retrieve(wing, room string, nResults int) string {
	if nResults <= 0 {
		nResults = 10
	}

	drawers := l.store.FilterByWingRoom(wing, room, nResults)
	if len(drawers) == 0 {
		label := ""
		if wing != "" {
			label += "wing=" + wing
		}
		if room != "" {
			if label != "" {
				label += " "
			}
			label += "room=" + room
		}
		return fmt.Sprintf("No drawers found for %s.", label)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("## L2 — ON-DEMAND (%d drawers)", len(drawers)))

	for _, d := range drawers {
		snippet := strings.ReplaceAll(strings.TrimSpace(d.Content), "\n", " ")
		if len(snippet) > 300 {
			snippet = snippet[:297] + "..."
		}
		entry := fmt.Sprintf("  [%s] %s", d.Room, snippet)
		if d.Source != "" {
			entry += fmt.Sprintf("  (%s)", d.Source)
		}
		lines = append(lines, entry)
	}

	return strings.Join(lines, "\n")
}

// -- Layer 3: Deep Search --
// Unlimited depth. Hybrid BM25 + vector semantic search via ChromaDB.

// Layer3 provides deep search using the hybrid searcher.
type Layer3 struct {
	searcher *Searcher
}

// NewLayer3 creates L3 deep search.
func NewLayer3(searcher *Searcher) *Layer3 {
	return &Layer3{searcher: searcher}
}

// Search performs deep search and returns formatted results.
func (l *Layer3) Search(query, wing, room string, nResults int) string {
	results := l.searcher.Search(query, wing, room, nResults)
	if len(results) == 0 {
		return "No results found."
	}

	var lines []string
	searchMode := "BM25"
	if l.searcher.IsChromaReady() {
		searchMode = "hybrid (vector+BM25)"
	}
	lines = append(lines, fmt.Sprintf("## L3 — SEARCH RESULTS for \"%s\" [%s]", query, searchMode))

	for i, r := range results {
		snippet := strings.ReplaceAll(strings.TrimSpace(r.Drawer.Content), "\n", " ")
		if len(snippet) > 300 {
			snippet = snippet[:297] + "..."
		}

		scoreDetail := fmt.Sprintf("score=%.3f, %s", r.Score, r.MatchType)
		if r.VectorScore > 0 {
			scoreDetail += fmt.Sprintf(", vec=%.3f, bm25=%.3f", r.VectorScore, r.BM25Score)
		}

		lines = append(lines, fmt.Sprintf("  [%d] %s/%s (%s)",
			i+1, r.Drawer.Wing, r.Drawer.Room, scoreDetail))
		lines = append(lines, fmt.Sprintf("      %s", snippet))
		if r.Drawer.Source != "" {
			lines = append(lines, fmt.Sprintf("      src: %s", r.Drawer.Source))
		}
	}

	return strings.Join(lines, "\n")
}

// SearchRaw returns raw results for programmatic use.
func (l *Layer3) SearchRaw(query, wing, room string, nResults int) []SearchResult {
	return l.searcher.Search(query, wing, room, nResults)
}

// -- MemoryStack: Unified 4-Layer Interface --

// MemoryStack wraps all 4 layers into a single interface.
type MemoryStack struct {
	L0 *Layer0
	L1 *Layer1
	L2 *Layer2
	L3 *Layer3
}

// NewMemoryStack creates the full stack.
func NewMemoryStack(palacePath string, store *DrawerStore, searcher *Searcher) *MemoryStack {
	return &MemoryStack{
		L0: NewLayer0(palacePath),
		L1: NewLayer1(store),
		L2: NewLayer2(store),
		L3: NewLayer3(searcher),
	}
}

// WakeUp returns L0 + L1 text (~600-900 tokens). Inject into system prompt.
func (ms *MemoryStack) WakeUp(wing string) string {
	parts := []string{
		ms.L0.Render(),
		"",
		ms.L1.Generate(wing),
	}
	return strings.Join(parts, "\n")
}

// Recall returns L2 on-demand retrieval.
func (ms *MemoryStack) Recall(wing, room string, nResults int) string {
	return ms.L2.Retrieve(wing, room, nResults)
}

// Search returns L3 deep search results.
func (ms *MemoryStack) Search(query, wing, room string, nResults int) string {
	return ms.L3.Search(query, wing, room, nResults)
}
