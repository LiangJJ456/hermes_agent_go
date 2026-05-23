package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// ParallelConfig configures a parallel node.
type ParallelConfig struct {
	Branches []*orchestrator.Graph `json:"Branches"`
}

// GraphExecutor executes a sub-graph. The orchestrator Executor implements this.
type GraphExecutor interface {
	Execute(ctx context.Context, g *orchestrator.Graph,
		input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error)
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

func init() {
	orchestrator.RegisterNodeType("parallel", &ParallelRunner{}, &ParallelConfig{})
}
