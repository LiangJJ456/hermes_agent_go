package executor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
	agcontext "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/context"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/trace"
)

// Executor walks a Graph, executing nodes and routing between them.
type Executor struct {
	Tracer trace.Tracer
}

// NewExecutor creates an executor with the given tracer.
func NewExecutor(tracer trace.Tracer) *Executor {
	if tracer == nil {
		tracer = &trace.NopTracer{}
	}
	return &Executor{Tracer: tracer}
}

// Execute runs a graph to completion or until interrupted.
// Returns (output, snapshot, error). snapshot is nil on normal completion.
func (e *Executor) Execute(ctx context.Context, g *orchestrator.Graph,
	input interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error) {

	ec := agcontext.NewExecutionContext(input)
	return e.executeFrom(ctx, g, g.StartAt, ec, 0)
}

// Resume continues execution from a saved snapshot after human input.
func (e *Executor) Resume(ctx context.Context, g *orchestrator.Graph,
	snap *orchestrator.ExecutionSnapshot, humanResponse interface{}) (interface{}, *orchestrator.ExecutionSnapshot, error) {

	wm, ok := snap.WorkMem.(*agcontext.WorkingMemory)
	if !ok {
		return nil, nil, fmt.Errorf("resume: invalid WorkMem in snapshot")
	}
	cm, ok := snap.ConvMem.(*agcontext.ConversationMemory)
	if !ok {
		return nil, nil, fmt.Errorf("resume: invalid ConvMem in snapshot")
	}

	ec := &agcontext.ExecutionContext{
		WorkMem: wm,
		ConvMem: cm,
	}

	// The humanResponse becomes the output of the interrupted node.
	ec.WorkMem.LastResult = humanResponse

	// Verify the interrupted node exists in the graph
	if _, ok := g.Nodes[snap.CurrentNode]; !ok {
		return nil, nil, fmt.Errorf("resume: node %q not found", snap.CurrentNode)
	}

	// Create a synthetic result for routing
	synthResult := &orchestrator.NodeResult{
		Status: orchestrator.StatusContinue,
		Output: humanResponse,
	}

	next, err := Route(ctx, snap.CurrentNode, synthResult, g.Edges, ec)
	if err != nil {
		return nil, nil, fmt.Errorf("resume route: %w", err)
	}

	return e.executeFrom(ctx, g, next, ec, snap.Step)
}

// executeFrom is the internal loop starting from a given node.
func (e *Executor) executeFrom(ctx context.Context, g *orchestrator.Graph,
	startNode string, ec *agcontext.ExecutionContext, startStep int) (interface{}, *orchestrator.ExecutionSnapshot, error) {

	currentNode := startNode
	step := startStep
	maxSteps := g.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 100
	}

	for currentNode != "" {
		step++
		if step > maxSteps {
			return nil, nil, fmt.Errorf("exceeded max steps (%d)", maxSteps)
		}

		node, ok := g.Nodes[currentNode]
		if !ok {
			return nil, nil, fmt.Errorf("node %q not found in graph", currentNode)
		}

		entry, ok := orchestrator.LookupNodeType(node.Type)
		if !ok {
			return nil, nil, fmt.Errorf("unknown node type: %q", node.Type)
		}

		if entry.Runner == nil {
			return nil, nil, fmt.Errorf("no runner registered for type %q", node.Type)
		}

		// Start span for this node
		spanCtx, span := e.Tracer.StartNodeSpan(ctx, currentNode, node.Type)
		span.SetAttribute("step", step)

		// Run with retry
		result, err := e.runWithRetry(spanCtx, node, entry.Runner, ec)
		if err != nil {
			e.Tracer.EndNodeSpan(span, err)
			if next := e.matchCatch(node.Catch, err); next != "" {
				currentNode = next
				continue
			}
			// Check global OnError
			if g.OnError != "" {
				currentNode = g.OnError
				continue
			}
			return nil, nil, fmt.Errorf("node %q: %w", currentNode, err)
		}

		e.Tracer.EndNodeSpan(span, nil)

		if result.Output != nil {
			ec.WorkMem.LastResult = result.Output
		}
		if node.OutputPath != "" && node.OutputPath != "$" {
			ec.WorkMem.State[node.OutputPath] = result.Output
		}

		// Check for interrupt (human-in-the-loop)
		if result.Interrupt {
			snap := &orchestrator.ExecutionSnapshot{
				ExecutionID: generateExecutionID(),
				CurrentNode: currentNode,
				Step:        step,
				WorkMem:     ec.WorkMem,
				ConvMem:     ec.ConvMem,
				CreatedAt:   time.Now(),
			}
			return result.Output, snap, nil
		}

		if result.Status == orchestrator.StatusEnd {
			return result.Output, nil, nil
		}

		next, err := Route(spanCtx, currentNode, result, g.Edges, ec)
		if err != nil {
			return nil, nil, fmt.Errorf("route from %q: %w", currentNode, err)
		}
		currentNode = next
	}

	return ec.WorkMem.LastResult, nil, nil
}

// runWithRetry executes a node with retry policy.
func (e *Executor) runWithRetry(ctx context.Context, node *orchestrator.NodeSpec,
	runner orchestrator.NodeRunner, ec *agcontext.ExecutionContext) (*orchestrator.NodeResult, error) {

	result, err := runner.Run(ctx, node, ec.WorkMem.LastResult, ec)
	if err == nil {
		return result, nil
	}
	if len(node.Retry) == 0 {
		return result, err
	}

	policy := node.Retry[0]
	for attempt := 1; attempt < policy.MaxAttempts; attempt++ {
		delay := time.Duration(policy.IntervalSeconds) * time.Second
		for i := 1; i < attempt; i++ {
			delay = time.Duration(float64(delay) * policy.BackoffRate)
		}
		if policy.Jitter > 0 {
			jitterNs := int64(float64(delay) * policy.Jitter)
			if jitterNs > 0 {
				b := make([]byte, 4)
				_, _ = rand.Read(b)
				offset := int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3])
				delay += time.Duration(offset % jitterNs)
			}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
		result, err = runner.Run(ctx, node, ec.WorkMem.LastResult, ec)
		if err == nil {
			return result, nil
		}
	}
	return result, err
}

// matchCatch finds the first catch policy matching the error.
func (e *Executor) matchCatch(policies []orchestrator.CatchPolicy, err error) string {
	if err == nil || len(policies) == 0 {
		return ""
	}
	errStr := err.Error()
	for _, p := range policies {
		for _, pattern := range p.ErrorEquals {
			if matchErr(errStr, pattern) {
				return p.Next
			}
		}
	}
	return ""
}

func matchErr(errStr, pattern string) bool {
	if len(pattern) == 0 || len(errStr) < len(pattern) {
		return false
	}
	for i := 0; i <= len(errStr)-len(pattern); i++ {
		if errStr[i:i+len(pattern)] == pattern {
			return true
		}
	}
	return false
}

func generateExecutionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
