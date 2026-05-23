package orchestrator

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// UnmarshalGraph loads a Graph from JSON bytes with two-phase parsing:
// phase 1 extracts Type, phase 2 parses Config using the registered prototype.
func UnmarshalGraph(data []byte) (*Graph, error) {
	var raw struct {
		Nodes     map[string]json.RawMessage `json:"Nodes"`
		Edges     []EdgeSpec                 `json:"Edges"`
		StartAt   string                     `json:"StartAt"`
		MaxSteps  int                        `json:"MaxSteps"`
		OnError   string                     `json:"OnError,omitempty"`
		OnTimeout string                     `json:"OnTimeout,omitempty"`
		Version   string                     `json:"Version,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal graph: %w", err)
	}

	g := &Graph{
		Edges:     raw.Edges,
		StartAt:   raw.StartAt,
		MaxSteps:  raw.MaxSteps,
		OnError:   raw.OnError,
		OnTimeout: raw.OnTimeout,
		Version:   raw.Version,
		Nodes:     make(map[string]*NodeSpec, len(raw.Nodes)),
	}

	if g.MaxSteps <= 0 {
		g.MaxSteps = 100
	}

	for name, rawNode := range raw.Nodes {
		node, err := unmarshalNode(rawNode)
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", name, err)
		}
		g.Nodes[name] = node
	}

	return g, nil
}

func unmarshalNode(raw json.RawMessage) (*NodeSpec, error) {
	var typeCheck struct {
		Type string `json:"Type"`
	}
	if err := json.Unmarshal(raw, &typeCheck); err != nil {
		return nil, fmt.Errorf("extract type: %w", err)
	}
	if typeCheck.Type == "" {
		return nil, fmt.Errorf("missing Type field")
	}

	entry, ok := LookupNodeType(typeCheck.Type)
	if !ok {
		return nil, fmt.Errorf("unknown node type: %q", typeCheck.Type)
	}

	var node NodeSpec
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, fmt.Errorf("unmarshal node: %w", err)
	}

	if entry.ConfigPrototype != nil && len(node.Config) > 0 {
		protoType := reflect.TypeOf(entry.ConfigPrototype)
		if protoType.Kind() == reflect.Ptr {
			protoType = protoType.Elem()
		}
		cfgPtr := reflect.New(protoType).Interface()
		if err := json.Unmarshal(node.Config, cfgPtr); err != nil {
			return nil, fmt.Errorf("unmarshal config for type %q: %w", typeCheck.Type, err)
		}
		node.ParsedConfig = cfgPtr
	}

	return &node, nil
}
