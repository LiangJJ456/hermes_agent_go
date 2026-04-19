package delegate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// delegateArgs delegate_task 工具参数
type delegateArgs struct {
	Task        string `json:"task"`                  // 子任务描述
	Context     string `json:"context,omitempty"`     // 额外上下文
	Constraints string `json:"constraints,omitempty"` // 约束条件
}

// Provider 子 Agent 委托提供者
// 持有父 Agent 的引用，用于创建子 Agent
type Provider struct {
	mu          sync.Mutex
	parentAgent *agent.AIAgent
	maxParallel int // 最大并行子 Agent 数
}

// NewProvider 创建委托提供者
func NewProvider(parentAgent *agent.AIAgent, maxParallel int) *Provider {
	if maxParallel <= 0 {
		maxParallel = 3
	}
	return &Provider{
		parentAgent: parentAgent,
		maxParallel: maxParallel,
	}
}

// Register 注册 delegate_task 工具到全局 Registry
func (p *Provider) Register() error {
	schema := types.ToolSchema{
		Type: "function",
		Function: types.FunctionSchema{
			Name: "delegate_task",
			Description: `Delegate a task to a sub-agent that runs independently with its own context.

Use this when:
- The task is self-contained and doesn't need interactive clarification
- You want to parallelize independent subtasks
- The task requires deep focus without polluting the main conversation

The sub-agent has access to the same tools (except delegate_task, clarify, memory_save) 
and will return its final result.

Constraints:
- Max delegation depth: 2 (parent → child → grandchild is rejected)
- Sub-agent has independent iteration budget (default 50)
- Sub-agent cannot interact with the user directly`,
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"task": {
						"type": "string",
						"description": "Clear, self-contained description of what the sub-agent should accomplish"
					},
					"context": {
						"type": "string",
						"description": "Additional context: file paths, variable values, constraints, or background info the sub-agent needs"
					},
					"constraints": {
						"type": "string",
						"description": "Specific constraints or requirements: output format, time limit, scope boundaries"
					}
				},
				"required": ["task"],
				"additionalProperties": false
			}`),
		},
	}

	return registry.Register(&registry.ToolEntry{
		Name:          "delegate_task",
		Toolset:       "agent",
		Schema:        schema,
		Handler:       p.handle,
		ParallelSafe:  true, // 多个委托可以并行
		NeverParallel: false,
		MaxResultSize: 50000, // 子 Agent 结果最大 50K 字符
	})
}

// handle 处理 delegate_task 调用
func (p *Provider) handle(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args delegateArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if args.Task == "" {
		return "", fmt.Errorf("task description is required")
	}

	// 创建子 Agent
	child, err := p.parentAgent.NewChildAgent(args.Task)
	if err != nil {
		return "", fmt.Errorf("create child agent: %w", err)
	}

	// 组装完整的任务描述
	fullTask := args.Task
	if args.Context != "" {
		fullTask += "\n\n## Context\n" + args.Context
	}
	if args.Constraints != "" {
		fullTask += "\n\n## Constraints\n" + args.Constraints
	}

	log.Info("delegating task to child agent",
		"task_preview", truncate(args.Task, 100),
		"depth", child.Depth(),
	)

	start := time.Now()

	// 运行子 Agent
	result, err := child.Run(ctx, fullTask)
	if err != nil {
		elapsed := time.Since(start)
		log.Warn("child agent failed",
			"error", err,
			"elapsed", elapsed,
			"iterations", child.GetStats().TotalIterations,
		)
		return fmt.Sprintf("Sub-agent failed after %s (%d iterations): %v",
			elapsed.Round(time.Millisecond), child.GetStats().TotalIterations, err), nil
	}

	elapsed := time.Since(start)
	stats := child.GetStats()
	log.Info("child agent completed",
		"elapsed", elapsed,
		"iterations", stats.TotalIterations,
		"tool_calls", stats.ToolCalls,
	)

	// 组装返回结果
	summary := fmt.Sprintf("## Sub-agent Result\n\n"+
		"**Task**: %s\n"+
		"**Elapsed**: %s | **Iterations**: %d | **Tool calls**: %d\n\n"+
		"---\n\n%s",
		truncate(args.Task, 200),
		elapsed.Round(time.Millisecond),
		stats.TotalIterations,
		stats.ToolCalls,
		result,
	)

	return summary, nil
}

// BatchDelegate 并行委托多个子任务
func (p *Provider) BatchDelegate(ctx context.Context, tasks []delegateArgs) []string {
	results := make([]string, len(tasks))
	sem := make(chan struct{}, p.maxParallel)
	var wg sync.WaitGroup

	for i, task := range tasks {
		i, task := i, task
		wg.Add(1)
		sem <- struct{}{} // 获取信号量
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			argsJSON, _ := json.Marshal(task)
			result, err := p.handle(ctx, argsJSON)
			if err != nil {
				results[i] = fmt.Sprintf("Error: %v", err)
			} else {
				results[i] = result
			}
		}()
	}

	wg.Wait()
	return results
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
