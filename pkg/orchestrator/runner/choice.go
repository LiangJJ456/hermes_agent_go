package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/condition"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// ChoiceConfig configures a choice (branch) node.
type ChoiceConfig struct {
	Choices []ChoiceEntry `json:"Choices"`
	Default string        `json:"Default,omitempty"`
}

// ChoiceEntry is a single branch. Condition is a string expression evaluated
// against the choice node's input; an empty Condition always matches.
type ChoiceEntry struct {
	Next      string `json:"Next"`
	Condition string `json:"Condition,omitempty"`
}

// Validate checks every choice condition at graph-load time.
func (c *ChoiceConfig) Validate() error {
	for _, ch := range c.Choices {
		if ch.Condition == "" {
			continue
		}
		if err := condition.Validate(ch.Condition); err != nil {
			return fmt.Errorf("choice -> %q: %w", ch.Next, err)
		}
	}
	return nil
}

// ChoiceRunner evaluates conditions and routes to the matching branch.
type ChoiceRunner struct{}

func (r *ChoiceRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg ChoiceConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ChoiceConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	scope := condition.Scope{Input: input}
	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.WorkMem != nil {
		scope.State = ec.WorkMem.State
	}

	for _, ch := range cfg.Choices {
		if ch.Condition == "" {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: ch.Next, Output: input}, nil
		}
		matched, err := condition.Evaluate(ch.Condition, scope)
		if err != nil {
			return nil, fmt.Errorf("choice condition %q: %w", ch.Condition, err)
		}
		if matched {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: ch.Next, Output: input}, nil
		}
	}

	if cfg.Default != "" {
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: cfg.Default, Output: input}, nil
	}

	return nil, fmt.Errorf("no choice matched and no default")
}

func init() {
	orchestrator.RegisterNodeType("choice", &ChoiceRunner{}, &ChoiceConfig{})
}
