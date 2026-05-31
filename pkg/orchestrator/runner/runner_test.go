package runner

import (
	"context"
	"encoding/json"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
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
		Config: json.RawMessage(`{"Choices":[{"Condition":"input.has_tool_calls == true","Next":"tools"}],"Default":"end"}`),
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
		Config: json.RawMessage(`{"Choices":[{"Condition":"input.has_tool_calls == true","Next":"tools"}],"Default":"end"}`),
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

func TestChoiceRunnerNumericMatch(t *testing.T) {
	r := &ChoiceRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Choices":[{"Condition":"input.count == 3","Next":"hit"}],"Default":"end"}`),
	}
	input := map[string]interface{}{"count": 3} // Go int vs literal 3 (float64)
	result, err := r.Run(context.Background(), node, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Next != "hit" {
		t.Fatalf("expected next 'hit', got %q", result.Next)
	}
}

type mockToolInvoker struct{}

func (m *mockToolInvoker) Invoke(ctx context.Context, resource string,
	input interface{}, timeout uint) (*orchestrator.NodeResult, error) {
	return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: "result"}, nil
}

func TestToolRunnerNoInvoker(t *testing.T) {
	r := &ToolRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Resource":"rpc/test"}`),
	}
	_, err := r.Run(context.Background(), node, "input", nil)
	if err == nil {
		t.Fatal("expected error for missing invoker")
	}
}

type mockGraphExecutor struct{}

func (m *mockGraphExecutor) Execute(ctx context.Context, g *orchestrator.Graph,
	input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error) {
	return nil, nil, nil
}

func TestParallelRunnerNoExecutor(t *testing.T) {
	r := &ParallelRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Branches":[]}`),
	}
	ec := agcontext.NewExecutionContext(nil) // no Executor set
	_, err := r.Run(context.Background(), node, nil, ec)
	if err == nil {
		t.Fatal("expected error for missing executor")
	}
}

func TestToolRunnerWithMockInvoker(t *testing.T) {
	r := &ToolRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Resource":"rpc/test","Timeout":10}`),
	}
	ec := agcontext.NewExecutionContext(nil)
	ec.ToolInvoker = &mockToolInvoker{}
	result, err := r.Run(context.Background(), node, "input", ec)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != orchestrator.StatusContinue {
		t.Fatalf("expected status 'continue', got %q", result.Status)
	}
	if result.Output != "result" {
		t.Fatalf("expected output 'result', got %v", result.Output)
	}
}

func TestHumanRunnerInterrupt(t *testing.T) {
	r := &HumanRunner{}
	node := &orchestrator.NodeSpec{
		Config: json.RawMessage(`{"Prompt":"Approve?","Actions":["yes","no"]}`),
	}
	result, err := r.Run(context.Background(), node, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Interrupt {
		t.Fatal("expected Interrupt=true")
	}
	if result.Status != orchestrator.StatusPending {
		t.Fatalf("expected status 'pending', got %q", result.Status)
	}
}
