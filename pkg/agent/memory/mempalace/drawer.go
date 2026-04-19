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

// Drawer represents a single memory unit stored in the palace.
type Drawer struct {
	ID         string            `json:"id"`
	Content    string            `json:"content"`
	Wing       string            `json:"wing"`
	Room       string            `json:"room"`
	Source     string            `json:"source,omitempty"` // source file / session
	Importance float64           `json:"importance"`       // 1-5 scale
	FiledAt    time.Time         `json:"filed_at"`
	Entities   []string          `json:"entities,omitempty"` // extracted entity names
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// DrawerCallback is called after a drawer is added or deleted.
// Used to propagate changes to ChromaDB or other indexing systems.
type DrawerCallback func(d *Drawer)

// DrawerStore manages persistent drawer storage on disk.
// Thread-safe. Each drawer is stored as an individual JSON file under
// {palacePath}/drawers/{wing}/{room}/{id}.json
type DrawerStore struct {
	mu         sync.RWMutex
	palacePath string

	// In-memory index for fast search
	index  map[string]*Drawer  // id -> drawer
	byWing map[string][]string // wing -> drawer IDs
	byRoom map[string][]string // room -> drawer IDs
	loaded bool

	// Callbacks for external indexing (e.g., ChromaDB)
	onAdd    DrawerCallback
	onDelete DrawerCallback
}

// NewDrawerStore creates a drawer store rooted at palacePath.
func NewDrawerStore(palacePath string) *DrawerStore {
	return &DrawerStore{
		palacePath: palacePath,
		index:      make(map[string]*Drawer),
		byWing:     make(map[string][]string),
		byRoom:     make(map[string][]string),
	}
}

// SetOnAdd registers a callback invoked after a drawer is added.
func (ds *DrawerStore) SetOnAdd(cb DrawerCallback) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.onAdd = cb
}

// SetOnDelete registers a callback invoked after a drawer is deleted.
func (ds *DrawerStore) SetOnDelete(cb DrawerCallback) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.onDelete = cb
}

// LoadAll loads all drawers from disk into memory index.
func (ds *DrawerStore) LoadAll() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.index = make(map[string]*Drawer)
	ds.byWing = make(map[string][]string)
	ds.byRoom = make(map[string][]string)

	drawersDir := filepath.Join(ds.palacePath, "drawers")
	if _, err := os.Stat(drawersDir); os.IsNotExist(err) {
		ds.loaded = true
		return nil
	}

	err := filepath.Walk(drawersDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		var d Drawer
		if err := json.Unmarshal(data, &d); err != nil {
			return nil // skip corrupted files
		}
		ds.indexDrawer(&d)
		return nil
	})

	ds.loaded = true
	return err
}

// Add stores a new drawer to disk and index.
func (ds *DrawerStore) Add(content, wing, room, source string, importance float64, entities []string) (*Drawer, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	if !ds.loaded {
		ds.mu.Unlock()
		ds.LoadAll()
		ds.mu.Lock()
	}

	wing = sanitizeName(wing)
	room = sanitizeName(room)
	if wing == "" {
		wing = "general"
	}
	if room == "" {
		room = "general"
	}

	id := generateDrawerID(content, wing, room)

	// Dedup check
	if _, exists := ds.index[id]; exists {
		return ds.index[id], nil
	}

	d := &Drawer{
		ID:         id,
		Content:    content,
		Wing:       wing,
		Room:       room,
		Source:     source,
		Importance: importance,
		FiledAt:    time.Now(),
		Entities:   entities,
		Metadata:   make(map[string]string),
	}

	if err := ds.saveToDisk(d); err != nil {
		return nil, fmt.Errorf("save drawer: %w", err)
	}

	ds.indexDrawer(d)

	// Fire callback outside critical path (still holding lock is fine;
	// the callback should be non-blocking or fast).
	if ds.onAdd != nil {
		ds.onAdd(d)
	}

	return d, nil
}

// Delete removes a drawer by ID.
func (ds *DrawerStore) Delete(id string) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	d, ok := ds.index[id]
	if !ok {
		return fmt.Errorf("drawer '%s' not found", id)
	}

	// Remove from disk
	path := ds.drawerPath(d)
	_ = os.Remove(path)

	// Remove from index
	delete(ds.index, id)
	ds.removeFromSlice(ds.byWing, d.Wing, id)
	ds.removeFromSlice(ds.byRoom, d.Room, id)

	// Fire callback
	if ds.onDelete != nil {
		ds.onDelete(d)
	}

	return nil
}

// Get returns a drawer by ID.
func (ds *DrawerStore) Get(id string) (*Drawer, bool) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	d, ok := ds.index[id]
	return d, ok
}

// ListWings returns all wing names with drawer counts.
func (ds *DrawerStore) ListWings() map[string]int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make(map[string]int, len(ds.byWing))
	for wing, ids := range ds.byWing {
		result[wing] = len(ids)
	}
	return result
}

// ListRooms returns rooms within a wing with drawer counts.
func (ds *DrawerStore) ListRooms(wing string) map[string]int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make(map[string]int)
	for _, id := range ds.byWing[wing] {
		if d, ok := ds.index[id]; ok {
			result[d.Room]++
		}
	}
	return result
}

// Count returns total number of drawers.
func (ds *DrawerStore) Count() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.index)
}

// TopDrawers returns the N highest-importance drawers, optionally filtered by wing.
func (ds *DrawerStore) TopDrawers(n int, wing string) []*Drawer {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var candidates []*Drawer
	for _, d := range ds.index {
		if wing != "" && d.Wing != wing {
			continue
		}
		candidates = append(candidates, d)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Importance != candidates[j].Importance {
			return candidates[i].Importance > candidates[j].Importance
		}
		return candidates[i].FiledAt.After(candidates[j].FiledAt)
	})

	if n > len(candidates) {
		n = len(candidates)
	}
	return candidates[:n]
}

// FilterByWingRoom returns drawers filtered by wing and/or room.
func (ds *DrawerStore) FilterByWingRoom(wing, room string, limit int) []*Drawer {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	var result []*Drawer
	for _, d := range ds.index {
		if wing != "" && d.Wing != wing {
			continue
		}
		if room != "" && d.Room != room {
			continue
		}
		result = append(result, d)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

// All returns all drawers (for search indexing).
func (ds *DrawerStore) All() []*Drawer {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	result := make([]*Drawer, 0, len(ds.index))
	for _, d := range ds.index {
		result = append(result, d)
	}
	return result
}

// -- Internal --

func (ds *DrawerStore) drawerPath(d *Drawer) string {
	return filepath.Join(ds.palacePath, "drawers", d.Wing, d.Room, d.ID+".json")
}

func (ds *DrawerStore) saveToDisk(d *Drawer) error {
	dir := filepath.Join(ds.palacePath, "drawers", d.Wing, d.Room)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, d.ID+".json")
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (ds *DrawerStore) indexDrawer(d *Drawer) {
	ds.index[d.ID] = d
	ds.byWing[d.Wing] = append(ds.byWing[d.Wing], d.ID)
	ds.byRoom[d.Room] = append(ds.byRoom[d.Room], d.ID)
}

func (ds *DrawerStore) removeFromSlice(m map[string][]string, key, val string) {
	slice := m[key]
	for i, v := range slice {
		if v == val {
			m[key] = append(slice[:i], slice[i+1:]...)
			return
		}
	}
}

func generateDrawerID(content, wing, room string) string {
	h := sha256.Sum256([]byte(content + "|" + wing + "|" + room))
	return "d_" + hex.EncodeToString(h[:12])
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	result = strings.Trim(result, "_")
	if result == "" {
		return "general"
	}
	return result
}
