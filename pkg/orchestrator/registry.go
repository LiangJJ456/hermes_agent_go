package orchestrator

import (
	"fmt"
	"sort"
	"sync"
)

type nodeTypeEntry struct {
	Runner          NodeRunner
	ConfigPrototype interface{}
}

var (
	mu       sync.RWMutex
	registry = make(map[string]*nodeTypeEntry)
)

// RegisterNodeType registers a node type with its config prototype.
func RegisterNodeType(name string, runner NodeRunner, proto interface{}) {
	mu.Lock()
	defer mu.Unlock()
	registry[name] = &nodeTypeEntry{Runner: runner, ConfigPrototype: proto}
}

// LookupNodeType returns the entry for a registered node type.
func LookupNodeType(name string) (*nodeTypeEntry, bool) {
	mu.RLock()
	defer mu.RUnlock()
	e, ok := registry[name]
	return e, ok
}

// MustLookupNodeType panics if the type is not registered.
func MustLookupNodeType(name string) *nodeTypeEntry {
	e, ok := LookupNodeType(name)
	if !ok {
		panic(fmt.Sprintf("node type %q not registered", name))
	}
	return e
}

// ListNodeTypes returns the names of all registered node types, sorted.
func ListNodeTypes() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
