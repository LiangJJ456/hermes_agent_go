package agent

import (
	"context"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// fakeModelProvider 返回一个无 tool_call 的终态响应,使 conversationLoop 一轮即结束。
type fakeModelProvider struct{}

func (fakeModelProvider) Name() string { return "fake" }
func (fakeModelProvider) Chat(_ context.Context, _ *model.ChatRequest) (*types.ChatResponse, error) {
	return &types.ChatResponse{Message: types.Message{Role: types.RoleAssistant, Content: "done"}}, nil
}
func (fakeModelProvider) ChatStream(_ context.Context, _ *model.ChatRequest, _ model.StreamCallback) (*types.ChatResponse, error) {
	return &types.ChatResponse{Message: types.Message{Role: types.RoleAssistant, Content: "done"}}, nil
}
func (fakeModelProvider) SupportsTools() bool   { return true }
func (fakeModelProvider) MaxContextTokens() int { return 128000 }

// fakeMemProvider 记录 Prefetch 调用次数与传入 query。
type fakeMemProvider struct {
	memory.BaseProvider
	prefetchCalls   int
	prefetchQueries []string
}

func (f *fakeMemProvider) Name() string                                          { return "fake-mem" }
func (f *fakeMemProvider) IsAvailable() bool                                     { return true }
func (f *fakeMemProvider) Initialize(_ context.Context, _ memory.InitOpts) error { return nil }
func (f *fakeMemProvider) Prefetch(_ context.Context, query string, _ string) string {
	f.prefetchCalls++
	f.prefetchQueries = append(f.prefetchQueries, query)
	return "recalled: " + query
}

func TestRunPrefetchesOncePerTurnWithUserInput(t *testing.T) {
	router := model.NewRouter()
	router.Register(fakeModelProvider{})

	cfg := types.AgentConfig{
		Model:            "fake/m",
		MaxIterations:    90,
		MaxParallelTools: 1,
		Platform:         "cli",
	}
	ag := NewAIAgent(cfg, router, registry.Global())

	mem := &fakeMemProvider{}
	mgr := memory.NewManager()
	if err := mgr.AddProvider(mem); err != nil {
		t.Fatalf("add provider: %v", err)
	}
	ag.SetMemoryManager(mgr)

	if _, err := ag.Run(context.Background(), "hello world"); err != nil {
		t.Fatalf("run: %v", err)
	}

	if mem.prefetchCalls != 1 {
		t.Fatalf("expected Prefetch called once, got %d", mem.prefetchCalls)
	}
	if len(mem.prefetchQueries) != 1 || mem.prefetchQueries[0] != "hello world" {
		t.Fatalf("expected query [hello world], got %v", mem.prefetchQueries)
	}
	if ag.pendingMemoryCtx != "" {
		t.Fatalf("expected pendingMemoryCtx cleared after turn, got %q", ag.pendingMemoryCtx)
	}
}
