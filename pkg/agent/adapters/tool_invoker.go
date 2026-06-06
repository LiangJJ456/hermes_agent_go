package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/memory"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
)

// CompressorInterface is a minimal interface for context compression.
type CompressorInterface interface {
	NeedsCompression(messages interface{}) bool
	Compress(ctx context.Context, messages interface{}) (interface{}, error)
}

// CompressFunc is called to compress conversation context in-place.
type CompressFunc func(ctx context.Context) error

// RegistryAdapter adapts hermes tool.Registry to runner.ToolInvoker.
type RegistryAdapter struct {
	Registry   *registry.Registry
	MemoryMgr  *memory.Manager
	Compressor CompressorInterface
	CompressFn CompressFunc

	// Background-tool wiring (injected by agent). When BackgroundAfter > 0 a
	// standard tool that runs longer than it is moved to the background and its
	// result is delivered later via Notify.
	Notify          func(xml string)
	BackgroundCtx   context.Context
	BackgroundAfter time.Duration
}

func (a *RegistryAdapter) Invoke(ctx context.Context, resource string,
	input interface{}, timeout uint) (*orchestrator.NodeResult, error) {

	// Human tool: return interrupt
	if resource == "builtin/ask_human" {
		return &orchestrator.NodeResult{
			Status:    orchestrator.StatusPending,
			Interrupt: true,
			Output:    input,
		}, nil
	}

	// Compress context tool
	if resource == "builtin/compress_context" {
		if a.CompressFn != nil {
			if err := a.CompressFn(ctx); err != nil {
				return &orchestrator.NodeResult{
					Status: orchestrator.StatusContinue,
					Output: fmt.Sprintf("compression failed: %v", err),
				}, nil
			}
		}
		return &orchestrator.NodeResult{
			Status: orchestrator.StatusContinue,
			Output: "context compressed",
		}, nil
	}

	// Check memory tools
	if a.MemoryMgr != nil && a.MemoryMgr.HasTool(resource) {
		args, _ := input.(map[string]interface{})
		if args == nil {
			args = make(map[string]interface{})
		}
		output, err := a.MemoryMgr.HandleToolCall(ctx, resource, args)
		if err != nil {
			return &orchestrator.NodeResult{
				Status: orchestrator.StatusContinue,
				Output: fmt.Sprintf("Error: %v", err),
			}, nil
		}
		return &orchestrator.NodeResult{
			Status: orchestrator.StatusContinue,
			Output: output,
		}, nil
	}

	// Standard tool: serialize input to JSON and call registry.
	argsJSON, err := json.Marshal(input)
	if err != nil {
		return &orchestrator.NodeResult{
			Status: orchestrator.StatusContinue,
			Output: fmt.Sprintf("Error: marshal args: %v", err),
		}, nil
	}

	// Explicit hard Timeout keeps the legacy cancel-on-timeout semantics.
	if timeout > 0 {
		ctxWithTO, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
		output, err := a.Registry.Execute(ctxWithTO, resource, argsJSON)
		if err != nil {
			return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: fmt.Sprintf("Error: %v", err)}, nil
		}
		return &orchestrator.NodeResult{Status: orchestrator.StatusContinue, Output: output}, nil
	}

	// No hard timeout: soft-timeout-to-background.
	return runStandardTool(ctx, a.BackgroundCtx, a.BackgroundAfter, a.Notify, resource, argsJSON, a.Registry.Execute)
}
