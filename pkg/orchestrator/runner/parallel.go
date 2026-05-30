package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
)

// ParallelConfig configures a parallel node.
type ParallelConfig struct {
	Branches []*orchestrator.Graph `json:"Branches"`
}

// ParallelRunner executes multiple branches concurrently.
type ParallelRunner struct {
	Executor GraphExecutor
}

// SetExecutor sets the graph executor.
func (r *ParallelRunner) SetExecutor(exec GraphExecutor) {
	r.Executor = exec
}

func (r *ParallelRunner) Run(ctx context.Context, node *orchestrator.NodeSpec,
	input interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var cfg ParallelConfig
	if node.ParsedConfig != nil {
		if c, ok := node.ParsedConfig.(*ParallelConfig); ok {
			cfg = *c
		}
	} else if len(node.Config) > 0 {
		json.Unmarshal(node.Config, &cfg)
	}

	if r.Executor == nil {
		return nil, fmt.Errorf("parallel runner: no executor configured")
	}

	// Dynamic tool dispatch: if branches are empty and input has tool_calls,
	// build one tool branch per tool_call automatically.
	if len(cfg.Branches) == 0 {
		if inputMap, ok := input.(map[string]interface{}); ok {
			if toolCalls, ok := inputMap["tool_calls"].([]map[string]interface{}); ok && len(toolCalls) > 0 {
				return r.dispatchToolCalls(ctx, toolCalls, execCtx)
			}
		}
	}

	// Static branches
	var wg sync.WaitGroup
	results := make([]interface{}, len(cfg.Branches))
	var firstErr error
	var mu sync.Mutex

	for i, branch := range cfg.Branches {
		if branch == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, g *orchestrator.Graph) {
			defer wg.Done()
			out, _, err := r.Executor.Execute(ctx, g, input)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			results[idx] = out
		}(i, branch)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: results,
	}, nil
}

// dispatchToolCalls dynamically dispatches tool calls from LLM output.
// Each tool_call is executed as a single-node sub-graph with ConvMem propagation.
func (r *ParallelRunner) dispatchToolCalls(ctx context.Context,
	toolCalls []map[string]interface{}, execCtx interface{}) (*orchestrator.NodeResult, error) {

	var wg sync.WaitGroup
	results := make([]interface{}, len(toolCalls))
	var firstErr error
	var mu sync.Mutex

	// Get the ConvMem-aware executor if available
	ctxExec, hasCtxExec := r.Executor.(ContextGraphExecutor)

	// Get parent ExecutionContext for ConvMem access
	var parentEC *agcontext.ExecutionContext
	if ec, ok := execCtx.(*agcontext.ExecutionContext); ok {
		parentEC = ec
	}

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(idx int, toolCall map[string]interface{}) {
			defer wg.Done()

			// Extract function name and arguments from tool_call
			var funcName, funcArgs, toolCallID string
			if id, ok := toolCall["id"].(string); ok {
				toolCallID = id
			}
			if fn, ok := toolCall["function"].(map[string]interface{}); ok {
				if n, ok := fn["name"].(string); ok {
					funcName = n
				}
				if a, ok := fn["arguments"].(string); ok {
					funcArgs = a
				}
			}

			if funcName == "" {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("tool_call missing function name")
				}
				mu.Unlock()
				return
			}

			// Build tool input: parse arguments JSON + add tool_call_id
			toolInput := map[string]interface{}{
				"tool_call_id": toolCallID,
			}
			if funcArgs != "" {
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(funcArgs), &parsed); err == nil {
					for k, v := range parsed {
						toolInput[k] = v
					}
				}
			}

			// Build a single-node sub-graph for this tool
			toolCfgBytes, _ := json.Marshal(ToolConfig{Resource: funcName})
			subGraph := &orchestrator.Graph{
				StartAt:  "tool",
				MaxSteps: 1,
				Nodes: map[string]*orchestrator.NodeSpec{
					"tool": {
						Type:   "tool",
						Config: toolCfgBytes,
					},
				},
			}

			// Execute with ConvMem if available
			var out interface{}
			var err error
			if hasCtxExec && parentEC != nil {
				childEC := &agcontext.ExecutionContext{
					WorkMem: agcontext.NewWorkingMemory(toolInput),
					ConvMem: parentEC.ConvMem, // shared ConvMem for tool result writes
				}
				out, _, err = ctxExec.ExecuteWithContext(ctx, subGraph, childEC)
			} else {
				out, _, err = r.Executor.Execute(ctx, subGraph, toolInput)
			}

			if err != nil {
				// Tool errors should not crash the entire parallel. Store error as result.
				results[idx] = map[string]interface{}{
					"tool_call_id": toolCallID,
					"name":         funcName,
					"error":        err.Error(),
				}
				return
			}
			results[idx] = out
		}(i, tc)
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: results,
	}, nil
}

func init() {
	orchestrator.RegisterNodeType("parallel", &ParallelRunner{}, &ParallelConfig{})
}
