package orchestrator

import "encoding/json"

// Graph is an executable DAG.
type Graph struct {
	Nodes     map[string]*NodeSpec `json:"Nodes"`
	Edges     []EdgeSpec           `json:"Edges"`
	StartAt   string               `json:"StartAt"`
	MaxSteps  int                  `json:"MaxSteps,omitempty"`
	OnError   string               `json:"OnError,omitempty"`
	OnTimeout string               `json:"OnTimeout,omitempty"`
	Version   string               `json:"Version,omitempty"`
}

// NodeSpec is the unified structure for a node. Type + Config determine behavior.
type NodeSpec struct {
	Type         string          `json:"Type"`
	Config       json.RawMessage `json:"Config,omitempty"`
	ParsedConfig interface{}     `json:"-"`
	InputPath    string          `json:"InputPath,omitempty"`
	OutputPath   string          `json:"OutputPath,omitempty"`
	Retry        []RetryPolicy   `json:"Retry,omitempty"`
	Catch        []CatchPolicy   `json:"Catch,omitempty"`
	Timeout      uint            `json:"Timeout,omitempty"`
}

// RetryPolicy configures automatic retries.
type RetryPolicy struct {
	MaxAttempts     int      `json:"MaxAttempts"`
	IntervalSeconds int      `json:"IntervalSeconds"`
	BackoffRate     float64  `json:"BackoffRate"`
	Jitter          float64  `json:"Jitter,omitempty"`
	Errors          []string `json:"Errors,omitempty"`
}

// CatchPolicy routes errors to a fallback node.
type CatchPolicy struct {
	ErrorEquals []string `json:"ErrorEquals"`
	Next        string   `json:"Next"`
}

// EdgeSpec defines a directed edge between nodes. Condition is an optional
// string expression; an edge with no Condition is an unconditional fallback.
type EdgeSpec struct {
	From      string `json:"From"`
	To        string `json:"To"`
	Condition string `json:"Condition,omitempty"`
	Priority  int    `json:"Priority"`
	Label     string `json:"Label,omitempty"`
}
