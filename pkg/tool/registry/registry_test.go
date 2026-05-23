package registry

import (
	"context"
	"encoding/json"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func newTestRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*ToolEntry),
	}
}

func TestRegistry_Register(t *testing.T) {
	reg := newTestRegistry()

	entry := &ToolEntry{
		Name:    "test_tool",
		Toolset: "test",
		Schema: types.ToolSchema{
			Function: types.FunctionSchema{
				Name:        "test_tool",
				Description: "A test tool",
			},
		},
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "ok", nil
		},
	}

	// First registration should succeed
	if err := reg.Register(entry); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Duplicate registration should fail
	if err := reg.Register(entry); err == nil {
		t.Error("expected error on duplicate registration")
	}
}

func TestRegistry_RegisterOrReplace(t *testing.T) {
	reg := newTestRegistry()

	entry1 := &ToolEntry{
		Name:    "tool_x",
		Toolset: "test",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "v1", nil
		},
	}
	reg.RegisterOrReplace(entry1)

	// Replace with new handler
	entry2 := &ToolEntry{
		Name:    "tool_x",
		Toolset: "test",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			return "v2", nil
		},
	}
	reg.RegisterOrReplace(entry2)

	// Should have the new handler
	result, err := reg.Execute(context.Background(), "tool_x", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "v2" {
		t.Errorf("expected v2, got %s", result)
	}

	// Order should only have one entry
	if reg.Count() != 1 {
		t.Errorf("expected 1 tool, got %d", reg.Count())
	}
}

func TestRegistry_Execute(t *testing.T) {
	reg := newTestRegistry()

	reg.RegisterOrReplace(&ToolEntry{
		Name:    "echo",
		Toolset: "test",
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a struct{ Msg string `json:"msg"` }
			json.Unmarshal(args, &a)
			return a.Msg, nil
		},
	})

	result, err := reg.Execute(context.Background(), "echo", json.RawMessage(`{"msg":"hello"}`))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected hello, got %s", result)
	}
}

func TestRegistry_Execute_NotFound(t *testing.T) {
	reg := newTestRegistry()

	_, err := reg.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for non-existent tool")
	}
}

func TestRegistry_GetSchemas_DisabledTools(t *testing.T) {
	reg := newTestRegistry()

	reg.RegisterOrReplace(&ToolEntry{Name: "tool_a", Toolset: "core"})
	reg.RegisterOrReplace(&ToolEntry{Name: "tool_b", Toolset: "core"})
	reg.RegisterOrReplace(&ToolEntry{Name: "tool_c", Toolset: "core"})

	schemas := reg.GetSchemas(nil, []string{"tool_b"})
	if len(schemas) != 2 {
		t.Errorf("expected 2 schemas (tool_b disabled), got %d", len(schemas))
	}
	for _, s := range schemas {
		if s.Function.Name == "tool_b" {
			t.Error("tool_b should be disabled")
		}
	}
}
