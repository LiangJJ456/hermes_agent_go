package orchestrator

import (
	"fmt"
	"sync"
)

type nodeTypeEntry struct {
	Runner         NodeRunner
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
