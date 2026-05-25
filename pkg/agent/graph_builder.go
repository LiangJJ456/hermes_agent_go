package agent

import (
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
          {"Condition": {"has_tool_calls": true}, "Next": "dispatch_tools"},
          {"Condition": {"needs_compression": true}, "Next": "compress"}
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

	return g, nil
}
