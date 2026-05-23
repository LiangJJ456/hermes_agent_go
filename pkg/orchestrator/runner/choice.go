package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ChoiceConfig configures a choice (branch) node.
type ChoiceConfig struct {
	Choices []ChoiceEntry `json:"Choices"`
	Default string        `json:"Default,omitempty"`
}

// ChoiceEntry is a single branch condition.
type ChoiceEntry struct {
	Next      string          `json:"Next"`
	Condition json.RawMessage `json:"Condition,omitempty"`
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

	for _, ch := range cfg.Choices {
		if len(ch.Condition) == 0 {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: ch.Next, Output: input}, nil
		}
		matched := evaluateCondition(ch.Condition, input)
		if matched {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: ch.Next, Output: input}, nil
		}
	}

	if cfg.Default != "" {
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Next: cfg.Default, Output: input}, nil
	}

	return nil, fmt.Errorf("no choice matched and no default")
}

// evaluateCondition checks if input matches the condition.
func evaluateCondition(cond json.RawMessage, input interface{}) bool {
	var condMap map[string]interface{}
	if err := json.Unmarshal(cond, &condMap); err != nil {
		return false
	}
	inputMap, ok := input.(map[string]interface{})
	if !ok {
		return false
	}
	for key, expectedVal := range condMap {
		actualVal, exists := inputMap[key]
		if !exists {
			return false
		}
		if actualVal != expectedVal {
			return false
		}
	}
	return true
}

func init() {
	orchestrator.RegisterNodeType("choice", &ChoiceRunner{}, &ChoiceConfig{})
}
