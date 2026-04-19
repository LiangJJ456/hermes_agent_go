package mempalace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Triple represents a subject → predicate → object relationship
// with temporal validity.
type Triple struct {
	ID         string    `json:"id"`
	Subject    string    `json:"subject"`
	Predicate  string    `json:"predicate"`
	Object     string    `json:"object"`
	ValidFrom  string    `json:"valid_from,omitempty"` // ISO date
	ValidTo    string    `json:"valid_to,omitempty"`   // ISO date (empty = still valid)
	Confidence float64   `json:"confidence"`
	Source     string    `json:"source,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Entity represents a node in the knowledge graph.
type Entity struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"` // person, project, tool, concept
	Properties map[string]string `json:"properties,omitempty"`
}

// KnowledgeGraph manages entities and their relationships with temporal validity.
// Persistence: entities.json + triples.json under {palacePath}/kg/
type KnowledgeGraph struct {
	mu         sync.RWMutex
	palacePath string

	entities map[string]*Entity // entity_id → entity
	triples  []*Triple          // all triples
	// Indexes for fast lookup
	bySubject   map[string][]*Triple // subject_id → triples
	byObject    map[string][]*Triple // object_id → triples
	byPredicate map[string][]*Triple // predicate → triples
}

// NewKnowledgeGraph creates a KG persisted under palacePath/kg/.
func NewKnowledgeGraph(palacePath string) *KnowledgeGraph {
	return &KnowledgeGraph{
		palacePath:  palacePath,
		entities:    make(map[string]*Entity),
		bySubject:   make(map[string][]*Triple),
		byObject:    make(map[string][]*Triple),
		byPredicate: make(map[string][]*Triple),
	}
}

// Load reads entities and triples from disk.
func (kg *KnowledgeGraph) Load() error {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	kgDir := filepath.Join(kg.palacePath, "kg")
	_ = os.MkdirAll(kgDir, 0o755)

	// Load entities
	entPath := filepath.Join(kgDir, "entities.json")
	if data, err := os.ReadFile(entPath); err == nil {
		var entities []*Entity
		if err := json.Unmarshal(data, &entities); err == nil {
			for _, e := range entities {
				kg.entities[e.ID] = e
			}
		}
	}

	// Load triples
	triPath := filepath.Join(kgDir, "triples.json")
	if data, err := os.ReadFile(triPath); err == nil {
		var triples []*Triple
		if err := json.Unmarshal(data, &triples); err == nil {
			kg.triples = triples
			for _, t := range triples {
				kg.bySubject[t.Subject] = append(kg.bySubject[t.Subject], t)
				kg.byObject[t.Object] = append(kg.byObject[t.Object], t)
				kg.byPredicate[t.Predicate] = append(kg.byPredicate[t.Predicate], t)
			}
		}
	}

	return nil
}

// AddEntity adds or updates an entity.
func (kg *KnowledgeGraph) AddEntity(name, entityType string, properties map[string]string) string {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	id := entityID(name)
	kg.entities[id] = &Entity{
		ID:         id,
		Name:       name,
		Type:       entityType,
		Properties: properties,
	}
	kg.saveLocked()
	return id
}

// AddTriple adds a relationship. Returns the triple ID.
// Deduplicates: if an identical active triple exists, returns its ID.
func (kg *KnowledgeGraph) AddTriple(subject, predicate, object string, validFrom string, confidence float64, source string) string {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	subID := entityID(subject)
	objID := entityID(object)
	pred := normalizePredicate(predicate)

	// Auto-create entities
	if _, ok := kg.entities[subID]; !ok {
		kg.entities[subID] = &Entity{ID: subID, Name: subject, Type: "unknown"}
	}
	if _, ok := kg.entities[objID]; !ok {
		kg.entities[objID] = &Entity{ID: objID, Name: object, Type: "unknown"}
	}

	// Dedup: check for identical active triple
	for _, t := range kg.bySubject[subID] {
		if t.Predicate == pred && t.Object == objID && t.ValidTo == "" {
			return t.ID
		}
	}

	id := tripleID(subID, pred, objID, validFrom)
	t := &Triple{
		ID:         id,
		Subject:    subID,
		Predicate:  pred,
		Object:     objID,
		ValidFrom:  validFrom,
		Confidence: confidence,
		Source:     source,
		CreatedAt:  time.Now(),
	}

	kg.triples = append(kg.triples, t)
	kg.bySubject[subID] = append(kg.bySubject[subID], t)
	kg.byObject[objID] = append(kg.byObject[objID], t)
	kg.byPredicate[pred] = append(kg.byPredicate[pred], t)

	kg.saveLocked()
	return id
}

// Invalidate marks a triple as no longer valid.
func (kg *KnowledgeGraph) Invalidate(subject, predicate, object string, endDate string) int {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	subID := entityID(subject)
	objID := entityID(object)
	pred := normalizePredicate(predicate)

	if endDate == "" {
		endDate = time.Now().Format("2006-01-02")
	}

	count := 0
	for _, t := range kg.bySubject[subID] {
		if t.Predicate == pred && t.Object == objID && t.ValidTo == "" {
			t.ValidTo = endDate
			count++
		}
	}

	if count > 0 {
		kg.saveLocked()
	}
	return count
}

// QueryEntity returns all relationships for an entity.
// direction: "outgoing", "incoming", "both"
// asOf: date filter (empty = all)
func (kg *KnowledgeGraph) QueryEntity(name string, direction string, asOf string) []map[string]any {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	id := entityID(name)
	var results []map[string]any

	if direction == "outgoing" || direction == "both" || direction == "" {
		for _, t := range kg.bySubject[id] {
			if asOf != "" && !isValidAt(t, asOf) {
				continue
			}
			results = append(results, kg.tripleToMap(t, "outgoing"))
		}
	}

	if direction == "incoming" || direction == "both" {
		for _, t := range kg.byObject[id] {
			if asOf != "" && !isValidAt(t, asOf) {
				continue
			}
			results = append(results, kg.tripleToMap(t, "incoming"))
		}
	}

	return results
}

// QueryRelationship returns all triples with a given predicate.
func (kg *KnowledgeGraph) QueryRelationship(predicate string, asOf string) []map[string]any {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	pred := normalizePredicate(predicate)
	var results []map[string]any
	for _, t := range kg.byPredicate[pred] {
		if asOf != "" && !isValidAt(t, asOf) {
			continue
		}
		results = append(results, kg.tripleToMap(t, ""))
	}
	return results
}

// Timeline returns all facts in chronological order.
func (kg *KnowledgeGraph) Timeline(entityName string, limit int) []map[string]any {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	var candidates []*Triple
	if entityName != "" {
		id := entityID(entityName)
		seen := make(map[string]bool)
		for _, t := range kg.bySubject[id] {
			if !seen[t.ID] {
				candidates = append(candidates, t)
				seen[t.ID] = true
			}
		}
		for _, t := range kg.byObject[id] {
			if !seen[t.ID] {
				candidates = append(candidates, t)
				seen[t.ID] = true
			}
		}
	} else {
		candidates = kg.triples
	}

	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i].ValidFrom, candidates[j].ValidFrom
		if a == "" {
			a = "9999"
		}
		if b == "" {
			b = "9999"
		}
		return a < b
	})

	if limit <= 0 {
		limit = 100
	}
	if limit > len(candidates) {
		limit = len(candidates)
	}

	var results []map[string]any
	for _, t := range candidates[:limit] {
		results = append(results, kg.tripleToMap(t, ""))
	}
	return results
}

// Stats returns counts.
func (kg *KnowledgeGraph) Stats() map[string]int {
	kg.mu.RLock()
	defer kg.mu.RUnlock()

	current := 0
	for _, t := range kg.triples {
		if t.ValidTo == "" {
			current++
		}
	}

	return map[string]int{
		"entities":        len(kg.entities),
		"triples":         len(kg.triples),
		"current_triples": current,
	}
}

// ── Internal ──

func (kg *KnowledgeGraph) tripleToMap(t *Triple, direction string) map[string]any {
	subName := t.Subject
	objName := t.Object
	if e, ok := kg.entities[t.Subject]; ok {
		subName = e.Name
	}
	if e, ok := kg.entities[t.Object]; ok {
		objName = e.Name
	}

	m := map[string]any{
		"subject":    subName,
		"predicate":  t.Predicate,
		"object":     objName,
		"valid_from": t.ValidFrom,
		"valid_to":   t.ValidTo,
		"current":    t.ValidTo == "",
		"confidence": t.Confidence,
	}
	if direction != "" {
		m["direction"] = direction
	}
	return m
}

func (kg *KnowledgeGraph) saveLocked() {
	kgDir := filepath.Join(kg.palacePath, "kg")
	_ = os.MkdirAll(kgDir, 0o755)

	// Save entities
	entList := make([]*Entity, 0, len(kg.entities))
	for _, e := range kg.entities {
		entList = append(entList, e)
	}
	if data, err := json.MarshalIndent(entList, "", "  "); err == nil {
		tmp := filepath.Join(kgDir, "entities.json.tmp")
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, filepath.Join(kgDir, "entities.json"))
		}
	}

	// Save triples
	if data, err := json.MarshalIndent(kg.triples, "", "  "); err == nil {
		tmp := filepath.Join(kgDir, "triples.json.tmp")
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, filepath.Join(kgDir, "triples.json"))
		}
	}
}

func isValidAt(t *Triple, asOf string) bool {
	if t.ValidFrom != "" && t.ValidFrom > asOf {
		return false
	}
	if t.ValidTo != "" && t.ValidTo < asOf {
		return false
	}
	return true
}

func entityID(name string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(name)), " ", "_")
}

func normalizePredicate(pred string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(pred)), " ", "_")
}

func tripleID(subID, pred, objID, validFrom string) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%d", subID, pred, objID, validFrom, time.Now().UnixNano())
	h := sha256.Sum256([]byte(raw))
	return "t_" + hex.EncodeToString(h[:12])
}
