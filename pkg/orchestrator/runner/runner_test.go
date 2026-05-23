package runner

import (
	"context"
	"encoding/json"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

func TestEndRunner(t *testing.T) {
	r := &EndRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Status":"success","Message":"done"}`),
	}
	result, err := r.Run(context.Background(), node, "world", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != orchestrator.StatusEnd {
		t.Fatalf("expected status 'end', got %q", result.Status)
	}
	m, ok := result.Output.(map[string]interface{})
	if !ok {
		t.Fatal("expected map output")
	}
	if m["Output"] != "world" {
		t.Fatalf("expected Output='world', got %v", m["Output"])
	}
}

func TestChoiceRunnerDefault(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Default":"fallback"}`),
	}
	result, err := r.Run(context.Background(), node, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "fallback" {
		t.Fatalf("expected next 'fallback', got %q", result.Next)
	}
}

func TestChoiceRunnerMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":{"has_tool_calls":true},"Next":"tools"}],"Default":"end"}`),
	}
	input := map[string]interface{}{"has_tool_calls": true}
	result, err := r.Run(context.Background(), node, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "tools" {
		t.Fatalf("expected next 'tools', got %q", result.Next)
	}
}

func TestChoiceRunnerNoMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":{"has_tool_calls":true},"Next":"tools"}],"Default":"end"}`),
	}
	input := map[string]interface{}{"has_tool_calls": false}
	result, err := r.Run(context.Background(), node, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "end" {
		t.Fatalf("expected default next 'end', got %q", result.Next)
	}
}
