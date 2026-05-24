package mempalace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// palaceProtocol is injected into status response so the model learns the
// memory interaction protocol on wake-up.
const palaceProtocol = `IMPORTANT — MemPalace Memory Protocol:
1. ON WAKE-UP: Palace overview is auto-loaded. Use palace_search before answering from memory.
2. BEFORE RESPONDING about any person, project, or past event: call palace_search or palace_kg_query FIRST. Never guess — verify.
3. AFTER EACH SESSION: the system auto-records a session diary.
4. WHEN FACTS CHANGE: call palace_kg_invalidate on the old fact, palace_kg_add for the new one.
5. TO STORE NEW MEMORIES: call palace_add to file content into the appropriate wing/room.`

// Provider implements memory.Provider using a MemPalace-style architecture:
//   - 4-Layer Memory Stack (L0 Identity -> L1 Essential -> L2 On-Demand -> L3 Deep Search)
//   - Knowledge Graph with temporal validity
//   - Hybrid BM25 + ChromaDB vector semantic search
//   - Automatic session diary on clean exit
type Provider struct {
	memory.BaseProvider // defaults for optional hooks

	mu         sync.Mutex
	palacePath string
	sessionID  string

	store    *DrawerStore
	kg       *KnowledgeGraph
	searcher *Searcher
	stack    *MemoryStack

	// Session tracking for diary
	turnCount     int
	sessionStart  time.Time
	sessionTopics []string // extracted from user messages
}

// New creates a MemPalace provider storing data under palacePath.
// Typical: ~/.hermes/palace
func New(palacePath string) *Provider {
	return &Provider{
		palacePath: palacePath,
	}
}

func (p *Provider) Name() string { return "mempalace" }

func (p *Provider) IsAvailable() bool {
	return p.palacePath != ""
}

// Initialize loads all palace data from disk and initializes ChromaDB.
func (p *Provider) Initialize(_ context.Context, opts memory.InitOpts) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.palacePath == "" {
		p.palacePath = filepath.Join(opts.HermesHome, "palace")
	}
	p.sessionID = opts.SessionID
	p.sessionStart = time.Now()

	// Ensure palace directory
	if err := os.MkdirAll(p.palacePath, 0o755); err != nil {
		return fmt.Errorf("create palace dir: %w", err)
	}

	// Initialize components
	p.store = NewDrawerStore(p.palacePath)
	if err := p.store.LoadAll(); err != nil {
		log.Warn("mempalace: drawer load error", "error", err)
	}

	p.kg = NewKnowledgeGraph(p.palacePath)
	if err := p.kg.Load(); err != nil {
		log.Warn("mempalace: kg load error", "error", err)
	}

	p.searcher = NewSearcher(p.store)

	// Initialize ChromaDB for vector search.
	// If ChromaDB init fails, the searcher silently degrades to pure BM25.
	if err := p.searcher.InitChroma(p.palacePath); err != nil {
		log.Warn("mempalace: ChromaDB init failed, falling back to BM25", "error", err)
	} else {
		log.Info("mempalace: ChromaDB initialized for hybrid search")

		// Wire up drawer store callbacks to keep ChromaDB in sync
		p.store.SetOnAdd(func(d *Drawer) {
			if err := p.searcher.IndexDrawer(d); err != nil {
				log.Warn("mempalace: ChromaDB index error", "drawer", d.ID, "error", err)
			}
		})
		p.store.SetOnDelete(func(d *Drawer) {
			if err := p.searcher.RemoveDrawer(d.ID); err != nil {
				log.Warn("mempalace: ChromaDB remove error", "drawer", d.ID, "error", err)
			}
		})

		// Reindex existing drawers into ChromaDB if collection is empty.
		// This handles first-time migration from pure JSON to ChromaDB.
		if err := p.searcher.ReindexAll(); err != nil {
			log.Warn("mempalace: ChromaDB reindex error", "error", err)
		}
	}

	p.stack = NewMemoryStack(p.palacePath, p.store, p.searcher)

	searchMode := "BM25 (fallback)"
	if p.searcher.IsChromaReady() {
		searchMode = "hybrid (vector+BM25)"
	}

	log.Info("mempalace: initialized",
		"palace", p.palacePath,
		"drawers", p.store.Count(),
		"kg", p.kg.Stats(),
		"search", searchMode)

	return nil
}

// SystemPromptBlock returns the wake-up text: L0 identity + L1 essential story + protocol.
func (p *Provider) SystemPromptBlock() string {
	if p.stack == nil {
		return ""
	}

	wakeUp := p.stack.WakeUp("")

	// Append KG stats if there are entities
	kgStats := p.kg.Stats()
	if kgStats["entities"] > 0 {
		wakeUp += fmt.Sprintf("\n\n## Knowledge Graph: %d entities, %d current facts",
			kgStats["entities"], kgStats["current_triples"])
	}

	// Indicate search mode
	if p.searcher.IsChromaReady() {
		wakeUp += "\n\n[Search: hybrid vector+BM25 via ChromaDB]"
	} else {
		wakeUp += "\n\n[Search: BM25 keyword matching (ChromaDB unavailable)]"
	}

	wakeUp += "\n\n" + palaceProtocol
	return wakeUp
}

// 预取相关性闸门参数
const (
	prefetchCandidatePool = 8    // L3 候选池大小(过滤前)
	prefetchTopN          = 3    // 过滤后注入上限
	prefetchMinVecSim     = 0.35 // 向量相似度下限
	prefetchBM25Epsilon   = 0.0  // BM25 命中阈值(>epsilon 视为真实 query 重叠)
)

// filterRelevant 仅保留有真实 query 命中信号的结果,剔除只靠 importance 撑分、
// query 零命中的抽屉,然后截断到 limit 条。
func filterRelevant(results []SearchResult, minVecSim, bm25Epsilon float64, limit int) []SearchResult {
	kept := make([]SearchResult, 0, len(results))
	for _, r := range results {
		if r.VectorScore >= minVecSim || r.BM25Score > bm25Epsilon {
			kept = append(kept, r)
		}
	}
	if len(kept) > limit {
		kept = kept[:limit]
	}
	return kept
}

// Prefetch searches the palace for context relevant to the current query.
func (p *Provider) Prefetch(_ context.Context, query string, _ string) string {
	if p.stack == nil || query == "" {
		return ""
	}

	// L3 search,然后按真实 query 相关性过滤(剔除仅 importance 撑分的抽屉)
	results := p.stack.L3.SearchRaw(query, "", "", prefetchCandidatePool)
	results = filterRelevant(results, prefetchMinVecSim, prefetchBM25Epsilon, prefetchTopN)
	if len(results) == 0 {
		return ""
	}

	var parts []string
	parts = append(parts, "## Palace Recall (relevant memories)")
	for _, r := range results {
		snippet := strings.TrimSpace(r.Drawer.Content)
		if len(snippet) > 300 {
			snippet = snippet[:297] + "..."
		}
		scoreInfo := fmt.Sprintf("%.3f", r.Score)
		if r.VectorScore > 0 {
			scoreInfo += fmt.Sprintf(" vec=%.3f", r.VectorScore)
		}
		parts = append(parts, fmt.Sprintf("  [%s/%s] (score=%s) %s",
			r.Drawer.Wing, r.Drawer.Room, scoreInfo, snippet))
	}

	// Also check KG for any entity mentioned in query
	kgContext := p.searchKGForQuery(query)
	if kgContext != "" {
		parts = append(parts, "", kgContext)
	}

	return strings.Join(parts, "\n")
}

// QueuePrefetch reloads from disk asynchronously to catch writes from other sessions.
func (p *Provider) QueuePrefetch(_, _ string) {
	go func() {
		if p.store == nil {
			return
		}
		_ = p.store.LoadAll()
	}()
}

// SyncTurn tracks conversation for diary writing.
func (p *Provider) SyncTurn(userContent, assistantContent, _ string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.turnCount++

	// Extract topic keywords from user message for diary
	tokens := tokenize(userContent)
	if len(tokens) > 5 {
		tokens = tokens[:5]
	}
	for _, t := range tokens {
		if len(t) > 3 {
			p.sessionTopics = append(p.sessionTopics, t)
		}
	}
}

// GetToolSchemas returns the MCP-style tools this provider exposes.
func (p *Provider) GetToolSchemas() []memory.ToolSchemaDef {
	return []memory.ToolSchemaDef{
		{
			Name: "palace_search",
			Description: "Search the memory palace using hybrid vector+keyword matching. Returns " +
				"relevant memories from the drawer archive. Use this BEFORE answering questions " +
				"about past events, people, projects, or anything that might be stored in memory.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Natural language search query.",
					},
					"wing": map[string]any{
						"type":        "string",
						"description": "Optional: filter by wing (project/topic area).",
					},
					"room": map[string]any{
						"type":        "string",
						"description": "Optional: filter by room (aspect within a wing).",
					},
					"n_results": map[string]any{
						"type":        "integer",
						"description": "Max results to return (default 5).",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "palace_add",
			Description: "Store a new memory (drawer) in the palace. Use for facts, discoveries, " +
				"preferences, or anything worth remembering across sessions.\n\n" +
				"WINGS are top-level categories (projects, topics). " +
				"ROOMS are aspects within a wing (e.g., 'technical', 'preferences', 'people').\n\n" +
				"Set importance 1-5: 1=trivial, 3=normal, 5=critical.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "The memory content to store (verbatim text).",
					},
					"wing": map[string]any{
						"type":        "string",
						"description": "Wing name (e.g., 'work', 'personal', 'project_x').",
					},
					"room": map[string]any{
						"type":        "string",
						"description": "Room name (e.g., 'preferences', 'people', 'technical').",
					},
					"importance": map[string]any{
						"type":        "number",
						"description": "Importance score 1-5 (default 3).",
					},
					"entities": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Entity names mentioned (people, projects, tools).",
					},
				},
				"required": []string{"content", "wing", "room"},
			},
		},
		{
			Name:        "palace_delete",
			Description: "Remove a drawer by ID. Use sparingly — only for incorrect or outdated entries.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"drawer_id": map[string]any{
						"type":        "string",
						"description": "The drawer ID to delete.",
					},
				},
				"required": []string{"drawer_id"},
			},
		},
		{
			Name:        "palace_status",
			Description: "Get palace overview: total drawers, wings with counts, KG stats, search mode.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name: "palace_recall",
			Description: "Retrieve memories filtered by wing and/or room (Layer 2 on-demand recall). " +
				"Use when a specific topic or project comes up in conversation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"wing": map[string]any{
						"type":        "string",
						"description": "Wing to recall from.",
					},
					"room": map[string]any{
						"type":        "string",
						"description": "Optional: specific room within the wing.",
					},
					"n_results": map[string]any{
						"type":        "integer",
						"description": "Max results (default 10).",
					},
				},
				"required": []string{"wing"},
			},
		},
		// Knowledge Graph Tools
		{
			Name: "palace_kg_add",
			Description: "Add a relationship fact to the knowledge graph.\n" +
				"Format: subject -> predicate -> object, with optional temporal validity.\n" +
				"Examples: (\"Alice\", \"works_on\", \"Project X\"), (\"Max\", \"child_of\", \"Alice\", valid_from=\"2015-04-01\")",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject": map[string]any{
						"type":        "string",
						"description": "The entity (person, project, tool) this fact is about.",
					},
					"predicate": map[string]any{
						"type":        "string",
						"description": "The relationship type (e.g., 'works_on', 'prefers', 'child_of').",
					},
					"object": map[string]any{
						"type":        "string",
						"description": "The target entity or value.",
					},
					"valid_from": map[string]any{
						"type":        "string",
						"description": "Optional: ISO date when this fact became true.",
					},
				},
				"required": []string{"subject", "predicate", "object"},
			},
		},
		{
			Name: "palace_kg_query",
			Description: "Query the knowledge graph for facts about an entity.\n" +
				"Returns all known relationships (outgoing, incoming, or both).\n" +
				"Use as_of to filter to facts valid at a specific date.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity": map[string]any{
						"type":        "string",
						"description": "Entity name to query.",
					},
					"direction": map[string]any{
						"type":        "string",
						"enum":        []string{"outgoing", "incoming", "both"},
						"description": "Relationship direction (default: both).",
					},
					"as_of": map[string]any{
						"type":        "string",
						"description": "Optional: only return facts valid at this date (ISO format).",
					},
				},
				"required": []string{"entity"},
			},
		},
		{
			Name: "palace_kg_invalidate",
			Description: "Mark a knowledge graph fact as no longer valid.\n" +
				"Sets the valid_to date. The fact remains in history but is marked as ended.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject": map[string]any{
						"type": "string",
					},
					"predicate": map[string]any{
						"type": "string",
					},
					"object": map[string]any{
						"type": "string",
					},
					"end_date": map[string]any{
						"type":        "string",
						"description": "Optional: ISO date when the fact ended (default: today).",
					},
				},
				"required": []string{"subject", "predicate", "object"},
			},
		},
	}
}

// HandleToolCall dispatches tool calls to the appropriate handler.
func (p *Provider) HandleToolCall(_ context.Context, toolName string, args map[string]any) (string, error) {
	switch toolName {
	case "palace_search":
		return p.handleSearch(args)
	case "palace_add":
		return p.handleAdd(args)
	case "palace_delete":
		return p.handleDelete(args)
	case "palace_status":
		return p.handleStatus()
	case "palace_recall":
		return p.handleRecall(args)
	case "palace_kg_add":
		return p.handleKGAdd(args)
	case "palace_kg_query":
		return p.handleKGQuery(args)
	case "palace_kg_invalidate":
		return p.handleKGInvalidate(args)
	default:
		return jsonResult(map[string]any{"error": fmt.Sprintf("unknown tool '%s'", toolName)}), nil
	}
}

// -- Hook Implementations --

// OnTurnStart logs the turn.
func (p *Provider) OnTurnStart(turnNumber int, message string, _ map[string]any) {
	log.Debug("mempalace: turn start", "turn", turnNumber, "msg_len", len(message))
}

// OnSessionEnd writes a diary entry summarizing the session.
func (p *Provider) OnSessionEnd(messages []map[string]any) {
	if p.store == nil || p.turnCount == 0 {
		return
	}

	diary := p.buildSessionDiary(messages)
	if diary == "" {
		return
	}

	_, err := p.store.Add(diary, "diary", "sessions", p.sessionID, 2.0, p.uniqueTopics())
	if err != nil {
		log.Warn("mempalace: diary write failed", "error", err)
	} else {
		log.Info("mempalace: session diary saved",
			"turns", p.turnCount,
			"topics", len(p.sessionTopics))
	}
}

// OnPreCompress scans messages about to be compressed for memory operations.
func (p *Provider) OnPreCompress(messages []map[string]any) string {
	memOps := 0
	for _, msg := range messages {
		content, _ := msg["content"].(string)
		if strings.Contains(content, "palace_add") || strings.Contains(content, "palace_kg_add") {
			memOps++
		}
	}
	if memOps == 0 {
		return ""
	}
	return fmt.Sprintf("[Palace: %d memory write(s) occurred in compressed context. "+
		"Use palace_search or palace_kg_query to access current state.]", memOps)
}

// OnMemoryWrite mirrors builtin memory writes into the palace.
func (p *Provider) OnMemoryWrite(action, target, content string) {
	if p.store == nil || action == "" || content == "" {
		return
	}

	// Mirror significant writes as drawers
	if action == "add" {
		_, _ = p.store.Add(
			fmt.Sprintf("[builtin/%s] %s", target, content),
			"builtin_mirror",
			target,
			"builtin_sync",
			2.0,
			nil,
		)
	}
}

// CleanExit closes ChromaDB and saves final state.
func (p *Provider) Shutdown() {
	if p.searcher != nil {
		p.searcher.CloseChroma()
	}
	log.Info("mempalace: clean exit",
		"drawers", p.store.Count(),
		"turns", p.turnCount)
}

// -- Tool Handlers --

func (p *Provider) handleSearch(args map[string]any) (string, error) {
	query, _ := args["query"].(string)
	wing, _ := args["wing"].(string)
	room, _ := args["room"].(string)
	nResults := intArg(args, "n_results", 5)

	if query == "" {
		return jsonResult(map[string]any{"error": "query is required"}), nil
	}

	results := p.searcher.Search(query, wing, room, nResults)
	if len(results) == 0 {
		return jsonResult(map[string]any{
			"results": []any{},
			"message": fmt.Sprintf("No results for '%s'", query),
		}), nil
	}

	searchMode := "bm25"
	if p.searcher.IsChromaReady() {
		searchMode = "hybrid"
	}

	var items []map[string]any
	for _, r := range results {
		item := map[string]any{
			"id":           r.Drawer.ID,
			"content":      r.Drawer.Content,
			"wing":         r.Drawer.Wing,
			"room":         r.Drawer.Room,
			"score":        r.Score,
			"vector_score": r.VectorScore,
			"bm25_score":   r.BM25Score,
			"match_type":   r.MatchType,
			"entities":     r.Drawer.Entities,
			"importance":   r.Drawer.Importance,
		}
		items = append(items, item)
	}

	return jsonResult(map[string]any{
		"results":     items,
		"count":       len(items),
		"search_mode": searchMode,
	}), nil
}

func (p *Provider) handleAdd(args map[string]any) (string, error) {
	content, _ := args["content"].(string)
	wing, _ := args["wing"].(string)
	room, _ := args["room"].(string)
	importance := floatArg(args, "importance", 3.0)

	var entities []string
	if entRaw, ok := args["entities"]; ok {
		if entList, ok := entRaw.([]any); ok {
			for _, e := range entList {
				if s, ok := e.(string); ok {
					entities = append(entities, s)
				}
			}
		}
	}

	if content == "" {
		return jsonResult(map[string]any{"error": "content is required"}), nil
	}
	if wing == "" {
		return jsonResult(map[string]any{"error": "wing is required"}), nil
	}

	drawer, err := p.store.Add(content, wing, room, p.sessionID, importance, entities)
	if err != nil {
		return jsonResult(map[string]any{"error": err.Error()}), nil
	}

	// Auto-create KG entities for referenced entities
	for _, ent := range entities {
		p.kg.AddEntity(ent, "auto", nil)
	}

	return jsonResult(map[string]any{
		"success":   true,
		"drawer_id": drawer.ID,
		"wing":      drawer.Wing,
		"room":      drawer.Room,
		"indexed":   p.searcher.IsChromaReady(),
		"message":   "Memory filed in palace.",
	}), nil
}

func (p *Provider) handleDelete(args map[string]any) (string, error) {
	drawerID, _ := args["drawer_id"].(string)
	if drawerID == "" {
		return jsonResult(map[string]any{"error": "drawer_id is required"}), nil
	}

	if err := p.store.Delete(drawerID); err != nil {
		return jsonResult(map[string]any{"error": err.Error()}), nil
	}

	return jsonResult(map[string]any{
		"success": true,
		"message": fmt.Sprintf("Drawer '%s' deleted.", drawerID),
	}), nil
}

func (p *Provider) handleStatus() (string, error) {
	wings := p.store.ListWings()
	kgStats := p.kg.Stats()

	searchMode := "bm25_fallback"
	if p.searcher.IsChromaReady() {
		searchMode = "hybrid_vector_bm25"
	}

	return jsonResult(map[string]any{
		"total_drawers":   p.store.Count(),
		"wings":           wings,
		"knowledge_graph": kgStats,
		"palace_path":     p.palacePath,
		"search_mode":     searchMode,
		"protocol":        palaceProtocol,
	}), nil
}

func (p *Provider) handleRecall(args map[string]any) (string, error) {
	wing, _ := args["wing"].(string)
	room, _ := args["room"].(string)
	nResults := intArg(args, "n_results", 10)

	text := p.stack.Recall(wing, room, nResults)
	return jsonResult(map[string]any{
		"content": text,
	}), nil
}

func (p *Provider) handleKGAdd(args map[string]any) (string, error) {
	subject, _ := args["subject"].(string)
	predicate, _ := args["predicate"].(string)
	object, _ := args["object"].(string)
	validFrom, _ := args["valid_from"].(string)

	if subject == "" || predicate == "" || object == "" {
		return jsonResult(map[string]any{"error": "subject, predicate, and object are required"}), nil
	}

	id := p.kg.AddTriple(subject, predicate, object, validFrom, 1.0, p.sessionID)

	return jsonResult(map[string]any{
		"success":   true,
		"triple_id": id,
		"message":   fmt.Sprintf("Added: %s -> %s -> %s", subject, predicate, object),
	}), nil
}

func (p *Provider) handleKGQuery(args map[string]any) (string, error) {
	entity, _ := args["entity"].(string)
	direction, _ := args["direction"].(string)
	asOf, _ := args["as_of"].(string)

	if entity == "" {
		return jsonResult(map[string]any{"error": "entity is required"}), nil
	}
	if direction == "" {
		direction = "both"
	}

	results := p.kg.QueryEntity(entity, direction, asOf)

	return jsonResult(map[string]any{
		"entity": entity,
		"facts":  results,
		"count":  len(results),
	}), nil
}

func (p *Provider) handleKGInvalidate(args map[string]any) (string, error) {
	subject, _ := args["subject"].(string)
	predicate, _ := args["predicate"].(string)
	object, _ := args["object"].(string)
	endDate, _ := args["end_date"].(string)

	if subject == "" || predicate == "" || object == "" {
		return jsonResult(map[string]any{"error": "subject, predicate, and object are required"}), nil
	}

	count := p.kg.Invalidate(subject, predicate, object, endDate)

	return jsonResult(map[string]any{
		"success":     true,
		"invalidated": count,
		"message":     fmt.Sprintf("Invalidated %d fact(s): %s -> %s -> %s", count, subject, predicate, object),
	}), nil
}

// -- Internal Helpers --

// searchKGForQuery checks if the query mentions any known KG entities.
func (p *Provider) searchKGForQuery(query string) string {
	queryLower := strings.ToLower(query)

	p.kg.mu.RLock()
	defer p.kg.mu.RUnlock()

	var matches []string
	for _, ent := range p.kg.entities {
		if strings.Contains(queryLower, strings.ToLower(ent.Name)) {
			// Found a matching entity — get its facts
			var facts []string
			for _, t := range p.kg.bySubject[ent.ID] {
				if t.ValidTo == "" { // only current facts
					objName := t.Object
					if e, ok := p.kg.entities[t.Object]; ok {
						objName = e.Name
					}
					facts = append(facts, fmt.Sprintf("  %s -> %s -> %s", ent.Name, t.Predicate, objName))
				}
			}
			if len(facts) > 0 {
				matches = append(matches, strings.Join(facts, "\n"))
			}
		}
	}

	if len(matches) == 0 {
		return ""
	}

	return "## Knowledge Graph (relevant facts)\n" + strings.Join(matches, "\n")
}

// buildSessionDiary creates a diary entry from the session messages.
func (p *Provider) buildSessionDiary(messages []map[string]any) string {
	if len(messages) == 0 {
		return ""
	}

	duration := time.Since(p.sessionStart).Round(time.Second)
	topics := p.uniqueTopics()

	var parts []string
	parts = append(parts, fmt.Sprintf("## Session Diary — %s", time.Now().Format("2006-01-02 15:04")))
	parts = append(parts, fmt.Sprintf("Duration: %s | Turns: %d", duration, p.turnCount))

	if len(topics) > 0 {
		parts = append(parts, fmt.Sprintf("Topics: %s", strings.Join(topics, ", ")))
	}

	// Extract user messages summary
	parts = append(parts, "\n### Conversation Summary")
	userMsgCount := 0
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "user" && content != "" {
			userMsgCount++
			preview := content
			if len(preview) > 200 {
				preview = preview[:200] + "..."
			}
			parts = append(parts, fmt.Sprintf("- User: %s", preview))
			// todo: cap diary entries to 10 messages
			// if userMsgCount >= 10 { // Cap diary entries
			// 	parts = append(parts, fmt.Sprintf("- ... and %d more messages", len(messages)-userMsgCount))
			// 	break
			// }
		}
	}

	return strings.Join(parts, "\n")
}

// uniqueTopics deduplicates and limits session topics.
func (p *Provider) uniqueTopics() []string {
	seen := make(map[string]bool)
	var unique []string
	for _, t := range p.sessionTopics {
		if !seen[t] {
			seen[t] = true
			unique = append(unique, t)
		}
	}
	if len(unique) > 10 {
		unique = unique[:10]
	}
	return unique
}

// -- JSON helpers --

func jsonResult(data map[string]any) string {
	b, _ := json.Marshal(data)
	return string(b)
}

func intArg(args map[string]any, key string, defaultVal int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return int(i)
			}
		}
	}
	return defaultVal
}

func floatArg(args map[string]any, key string, defaultVal float64) float64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case json.Number:
			if f, err := n.Float64(); err == nil {
				return f
			}
		}
	}
	return defaultVal
}
