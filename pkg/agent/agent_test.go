package agent

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	orchexec "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/executor"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func TestGraphAgentSimpleReply(t *testing.T) {
	// Build minimal graph: LLM → End
	g := &orchestrator.Graph{
		StartAt:  "llm",
		MaxSteps: 5,
		Nodes: map[string]*orchestrator.NodeSpec{
			"llm": {Type: "llm"},
			"end": {Type: "end"},
		},
		Edges: []orchestrator.EdgeSpec{
			{From: "llm", To: "end", Priority: 0},
		},
	}

	// Set up mock LLM invoker
	mockLLM := &mockLLMInvoker{reply: "hello from mock"}
	entry, ok := orchestrator.LookupNodeType("llm")
	if !ok {
		t.Fatal("llm node type not registered")
	}
	runner, ok := entry.Runner.(*runner.LLMRunner)
	if !ok {
		t.Fatal("llm runner not found")
	}
	runner.SetInvoker(mockLLM)

	exec := orchexec.NewExecutor(nil)
	output, snap, err := exec.Execute(context.Background(), g, "hi")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if snap != nil {
		t.Fatal("expected snap=nil for simple reply")
	}
	m, ok := output.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map output, got %T: %v", output, output)
	}
	// End node wraps output: {"Status","Message","Output"}
	inner, ok := m["Output"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested Output map, got %T: %v", m["Output"], m["Output"])
	}
	if inner["content"] != "hello from mock" {
		t.Fatalf("expected 'hello from mock', got %v", inner["content"])
	}
}

type mockLLMInvoker struct {
	reply string
}

func (m *mockLLMInvoker) Chat(ctx context.Context, model string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig) (*orchestrator.NodeResult, error) {
	return &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: map[string]interface{}{
			"content":        m.reply,
			"has_tool_calls": false,
		},
	}, nil
}

func (m *mockLLMInvoker) ChatStream(ctx context.Context, model string,
	messages []runner.LLMMessage, tools []string,
	cfg runner.LLMConfig, onDelta func(string)) (*orchestrator.NodeResult, error) {
	onDelta(m.reply)
	return m.Chat(ctx, model, messages, tools, cfg)
}
