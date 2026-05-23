package context

import (
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func TestIsTodoReminder(t *testing.T) {
	tests := []struct {
		msg  types.Message
		want bool
	}{
		{types.Message{Role: types.RoleSystem, Content: "<todo-reminder>\nsome plan\n</todo-reminder>"}, true},
		{types.Message{Role: types.RoleSystem, Content: "normal system message"}, false},
		{types.Message{Role: types.RoleAssistant, Content: "<todo-reminder>"}, false}, // wrong role
		{types.Message{Role: types.RoleSystem, Content: ""}, false},
	}

	for i, tt := range tests {
		got := isTodoReminder(tt.msg)
		if got != tt.want {
			t.Errorf("test %d: isTodoReminder = %v, want %v", i, got, tt.want)
		}
	}
}

func TestRemoveTodoReminders(t *testing.T) {
	c := &Compressor{}
	msgs := []types.Message{
		{Role: types.RoleUser, Content: "hello"},
		{Role: types.RoleSystem, Content: "<todo-reminder>\nplan\n</todo-reminder>"},
		{Role: types.RoleAssistant, Content: "I'll help"},
		{Role: types.RoleSystem, Content: "<todo-reminder>\nupdated plan\n</todo-reminder>"},
	}

	result := c.removeTodoReminders(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != types.RoleUser {
		t.Errorf("expected user message first, got %s", result[0].Role)
	}
}

func TestExtractLastTodoReminder(t *testing.T) {
	c := &Compressor{}

	// No reminder
	msgs := []types.Message{
		{Role: types.RoleUser, Content: "hi"},
	}
	if r := c.extractLastTodoReminder(msgs); r != nil {
		t.Error("expected nil for no reminders")
	}

	// Multiple reminders - should get last
	msgs = []types.Message{
		{Role: types.RoleSystem, Content: "<todo-reminder>\nfirst\n</todo-reminder>"},
		{Role: types.RoleUser, Content: "test"},
		{Role: types.RoleSystem, Content: "<todo-reminder>\nsecond\n</todo-reminder>"},
	}
	r := c.extractLastTodoReminder(msgs)
	if r == nil {
		t.Fatal("expected non-nil")
	}
	if r.Content != "<todo-reminder>\nsecond\n</todo-reminder>" {
		t.Errorf("got wrong reminder: %s", r.Content)
	}
}

func TestProtectTail_ToolCallChain(t *testing.T) {
	c := &Compressor{}

	msgs := []types.Message{
		{Role: types.RoleUser, Content: "do something"},
		{Role: types.RoleAssistant, Content: "", ToolCalls: []types.ToolCall{{ID: "tc_1"}}},
		{Role: types.RoleTool, Content: "result 1", ToolCallID: "tc_1"},
		{Role: types.RoleAssistant, Content: "done"},
	}

	// Protect just the last 2 messages by token budget
	// The tool result should pull in the assistant tool_calls message
	tail := c.protectTail(msgs, 50)
	
	// Should include at least the assistant+tool pair
	hasAssistantTC := false
	hasToolResult := false
	for _, m := range tail {
		if m.Role == types.RoleAssistant && len(m.ToolCalls) > 0 {
			hasAssistantTC = true
		}
		if m.Role == types.RoleTool && m.ToolCallID == "tc_1" {
			hasToolResult = true
		}
	}

	if hasToolResult && !hasAssistantTC {
		t.Error("tool result in tail without corresponding assistant tool_calls message — chain broken")
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []types.Message{
		{Role: types.RoleSystem, Content: "You are helpful"},   // ~4 chars/token + overhead
		{Role: types.RoleUser, Content: "Hello"},
	}
	
	tokens := estimateTokens(msgs)
	if tokens <= 0 {
		t.Error("expected positive token count")
	}
	
	// Rough check: "You are helpful" = 15 chars ≈ 3-4 tokens + overhead
	// "Hello" = 5 chars ≈ 1-2 tokens + overhead
	if tokens > 100 {
		t.Errorf("token estimate too high: %d", tokens)
	}
}
