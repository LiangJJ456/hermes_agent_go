package agent

import (
	"context"
	"strings"
	"testing"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// fakeModelProvider 返回一个无 tool_call 的终态响应,使 conversationLoop 一轮即结束。
// 使用指针接收者以捕获最后一次收到的请求供断言使用。
type fakeModelProvider struct {
	lastReq *model.ChatRequest
}

func (f *fakeModelProvider) Name() string { return "fake" }
func (f *fakeModelProvider) Chat(_ context.Context, req *model.ChatRequest) (*types.ChatResponse, error) {
	f.lastReq = req
	return &types.ChatResponse{Message: types.Message{Role: types.RoleAssistant, Content: "done"}}, nil
}
func (f *fakeModelProvider) ChatStream(_ context.Context, req *model.ChatRequest, _ model.StreamCallback) (*types.ChatResponse, error) {
	f.lastReq = req
	return &types.ChatResponse{Message: types.Message{Role: types.RoleAssistant, Content: "done"}}, nil
}
func (f *fakeModelProvider) SupportsTools() bool   { return true }
func (f *fakeModelProvider) MaxContextTokens() int { return 128000 }

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
	prov := &fakeModelProvider{}
	router.Register(prov)

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

	if prov.lastReq == nil {
		t.Fatal("model provider never received a request")
	}
	sysIdx, userIdx := -1, -1
	for i, m := range prov.lastReq.Messages {
		if m.Role == types.RoleSystem && strings.Contains(m.Content, "recalled: hello world") {
			sysIdx = i
		}
		if m.Role == types.RoleUser && m.Content == "hello world" {
			userIdx = i
		}
	}
	if sysIdx < 0 {
		t.Fatal("memory context block was not injected into the model request")
	}
	if userIdx < 0 || sysIdx >= userIdx {
		t.Fatalf("memory context should be injected before the user message (sysIdx=%d, userIdx=%d)", sysIdx, userIdx)
	}
}
