package agent

import (
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
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
          {"Condition": {"has_tool_calls": true}, "Next": "dispatch_tools"}
        ],
        "Default": "end"
      }
    },
    "dispatch_tools": {
      "Type": "parallel",
      "Config": {"Branches": "$dynamic_tool_branches"}
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
    {"From": "wait_and_retry", "To": "llm", "Priority": 0}
  ]
}`

// BuildDefaultGraph returns the default agent graph. The cfg values
// (MaxIterations, Model, Temperature, MaxTokens) are used to override
// defaults in the graph definition.
func BuildDefaultGraph(cfg types.AgentConfig) (*orchestrator.Graph, error) {
	g, err := orchestrator.UnmarshalGraph([]byte(defaultGraphJSON))
	if err != nil {
		return nil, err
	}

	g.MaxSteps = cfg.MaxIterations

	return g, nil
}
