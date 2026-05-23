package runner

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// HumanConfig configures a human-in-the-loop node.
type HumanConfig struct {
	Prompt  string      `json:"Prompt,omitempty"`
	Actions []string    `json:"Actions,omitempty"`
	Timeout uint        `json:"Timeout,omitempty"`
	Schema  interface{} `json:"Schema,omitempty"`
}

// HumanRunner pauses execution and waits for human input.
type HumanRunner struct{}

func (r *HumanRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg HumanConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*HumanConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	return &orchestrator.NodeResult{
		Status:    orchestrator.StatusPending,
		Interrupt: true,
		Output:    input,
	}, nil
}

func init() {
	orchestrator.RegisterNodeType("human", &HumanRunner{}, &HumanConfig{})
}
