package runner

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// EndConfig is the configuration for an end node.
type EndConfig struct {
	Status  string `json:"Status"`
	Message string `json:"Message,omitempty"`
}

// EndRunner terminates graph execution.
type EndRunner struct{}

func (r *EndRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg EndConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*EndConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}
	if cfg.Status == "" {
		cfg.Status = "success"
	}
	return &orchestrator.NodeResult{
		Status: orchestrator.StatusEnd,
		Output: map[string]interface{}{
			"Status":  cfg.Status,
			"Message": cfg.Message,
			"Output":  input,
		},
	}, nil
}

func init() {
	orchestrator.RegisterNodeType("end", &EndRunner{}, &EndConfig{})
}
