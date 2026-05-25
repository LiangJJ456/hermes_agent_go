package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// LLMConfig configures an llm node.
type LLMConfig struct {
	Model        string          `json:"Model"`
	SystemPrompt string          `json:"SystemPrompt,omitempty"`
	UserPrompt   string          `json:"UserPrompt,omitempty"`
	Tools        []string        `json:"Tools,omitempty"`
	OutputSchema json.RawMessage `json:"OutputSchema,omitempty"`
	Temperature  float64         `json:"Temperature,omitempty"`
	MaxTokens    int             `json:"MaxTokens,omitempty"`
}

// LLMMessage is a single message sent to the LLM.
type LLMMessage struct {
	Role    string `json:"Role"`
	Content string `json:"Content"`
	Name    string `json:"Name,omitempty"`
}

// LLMInvoker abstracts the actual LLM call. hermes adapts model.Router to this.
type LLMInvoker interface {
	Chat(ctx context.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig) (*orchestrator.NodeResult, error)
	ChatStream(ctx context.Context, model string, messages []LLMMessage,
		tools []string, cfg LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error)
}

// LLMRunner executes an LLM node by delegating to an LLMInvoker.
type LLMRunner struct {
	Invoker LLMInvoker
}

// SetInvoker sets the LLM invoker (called after construction).
func (r *LLMRunner) SetInvoker(inv LLMInvoker) {
	r.Invoker = inv
}

func (r *LLMRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg LLMConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*LLMConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Invoker == nil {
		return nil, fmt.Errorf("llm runner: no invoker configured")
	}

	// Build messages: prefer full conversation history from ConvMem
	var messages []LLMMessage

	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok && ec.ConvMem != nil && len(ec.ConvMem.Messages) > 0 {
		// Use full conversation history (system + all user/assistant turns)
		for _, m := range ec.ConvMem.Messages {
			messages = append(messages, LLMMessage{Role: m.Role, Content: m.Content, Name: m.Name})
		}
	} else {
		// Fallback: single-turn (backward compatible)
		if cfg.SystemPrompt != "" {
			messages = append(messages, LLMMessage{Role: "system", Content: cfg.SystemPrompt})
		}
		userContent := formatInput(input)
		messages = append(messages, LLMMessage{Role: "user", Content: userContent})
	}

	return r.Invoker.Chat(ctx, cfg.Model, messages, cfg.Tools, cfg)
}

func formatInput(input interface{}) string {
	if input == nil {
		return ""
	}
	if s, ok := input.(string); ok {
		return s
	}
	b, _ := json.Marshal(input)
	return string(b)
}

func init() {
	orchestrator.RegisterNodeType("llm", &LLMRunner{}, &LLMConfig{})
}
