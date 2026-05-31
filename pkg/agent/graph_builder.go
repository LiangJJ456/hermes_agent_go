package agent

import (
	"fmt"
	"os"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchrunner "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// defaultGraphJSON is the built-in agent graph definition.
const defaultGraphJSON = `{
  "StartAt": "llm",
  "MaxSteps": 90,
  "Nodes": {
    "llm": {
      "Type": "llm",
      "Config": {"Model": "$model", "Temperature": 0.7},
      "Retry": [{"MaxAttempts": 3, "IntervalSeconds": 2, "BackoffRate": 2}],
      "Catch": [{"ErrorEquals": ["rate_limited"], "Next": "wait_and_retry"}]
    },
    "route": {
      "Type": "choice",
      "Config": {
        "Choices": [
          {"Condition": "input.has_tool_calls == true", "Next": "dispatch_tools"},
          {"Condition": "input.needs_compression == true", "Next": "compress"}
        ],
        "Default": "end"
      }
    },
    "dispatch_tools": {
      "Type": "parallel",
      "Config": {"Branches": []}
    },
    "compress": {
      "Type": "tool",
      "Config": {"Resource": "builtin/compress_context"}
    },
    "wait_and_retry": {
      "Type": "tool",
      "Config": {"Resource": "builtin/wait", "Parameters": {"seconds": 5}}
    },
    "end": {"Type": "end"}
  },
  "Edges": [
    {"From": "llm", "To": "route", "Priority": 0},
    {"From": "dispatch_tools", "To": "llm", "Priority": 0},
    {"From": "compress", "To": "llm", "Priority": 0},
    {"From": "wait_and_retry", "To": "llm", "Priority": 0}
  ]
}`

// BuildGraph returns the orchestrator graph for the agent. When cfg.GraphPath
// is set, the graph is loaded from that file; otherwise the built-in default
// graph is used. In both cases the cfg values (MaxIterations, Temperature,
// MaxTokens) are applied as overrides where applicable.
func BuildGraph(cfg types.AgentConfig) (*orchestrator.Graph, error) {
	if cfg.GraphPath == "" {
		return BuildDefaultGraph(cfg)
	}

	data, err := os.ReadFile(cfg.GraphPath)
	if err != nil {
		return nil, fmt.Errorf("read custom graph %q: %w", cfg.GraphPath, err)
	}

	g, err := orchestrator.UnmarshalGraph(data)
	if err != nil {
		return nil, fmt.Errorf("parse custom graph %q: %w", cfg.GraphPath, err)
	}

	applyConfigOverrides(g, cfg)
	return g, nil
}

// BuildDefaultGraph returns the default agent graph. The cfg values
// (MaxIterations, Model, Temperature, MaxTokens) are used to override
// defaults in the graph definition.
func BuildDefaultGraph(cfg types.AgentConfig) (*orchestrator.Graph, error) {
	// 替换占位符
	resolved := defaultGraphJSON
	if cfg.Model != "" {
		resolved = strings.ReplaceAll(resolved, "$model", cfg.Model)
	}

	g, err := orchestrator.UnmarshalGraph([]byte(resolved))
	if err != nil {
		return nil, err
	}

	applyConfigOverrides(g, cfg)
	return g, nil
}

// applyConfigOverrides applies cfg-derived overrides onto a parsed graph.
// All overrides are guarded so they are no-ops when the relevant node or
// value is absent, making this safe for arbitrary user-supplied graphs.
func applyConfigOverrides(g *orchestrator.Graph, cfg types.AgentConfig) {
	// 覆盖 MaxSteps
	if cfg.MaxIterations > 0 {
		g.MaxSteps = cfg.MaxIterations
	}

	// 覆盖 LLM 节点参数
	if llmNode, ok := g.Nodes["llm"]; ok {
		if llmCfg, ok := llmNode.ParsedConfig.(*orchrunner.LLMConfig); ok {
			if cfg.Temperature > 0 {
				llmCfg.Temperature = cfg.Temperature
			}
			if cfg.MaxTokens > 0 {
				llmCfg.MaxTokens = cfg.MaxTokens
			}
		}
	}
}
