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

// buildFullTask assembles the complete task prompt from delegate arguments.
func buildFullTask(args delegateArgs) string {
	fullTask := args.Task
	if args.Context != "" {
		fullTask += "\n\n## Context\n" + args.Context
	}
	if args.Constraints != "" {
		fullTask += "\n\n## Constraints\n" + args.Constraints
	}
	return fullTask
}

// buildTaskNotification formats a completed (or failed) child-agent result as
// an XML <task-notification> block that the parent LLM can parse.
func buildTaskNotification(taskID, task, result string, err error, elapsed time.Duration, stats agent.Stats) string {
	status := "completed"
	if err != nil {
		status = "failed"
		result = err.Error()
	}
	return fmt.Sprintf(`<task-notification>
<task-id>%s</task-id>
<status>%s</status>
<task>%s</task>
<elapsed>%s</elapsed>
<iterations>%d</iterations>
<tool-calls>%d</tool-calls>
<result>%s</result>
</task-notification>`,
		taskID, status, task,
		elapsed.Round(time.Millisecond).String(),
		stats.TotalIterations,
		stats.ToolCalls,
		result,
	)
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

	fullTask := buildFullTask(args)

	log.Info("delegating task to child agent",
		"task_preview", truncate(args.Task, 100),
		"depth", child.Depth(),
	)

	start := time.Now()

	// 运行子 Agent
	result, _, err := child.Run(ctx, fullTask)
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

// handleAsync executes a child agent in a background goroutine and returns
// immediately with a task ID. The child's result is delivered via AddNotification.
func (p *Provider) handleAsync(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args delegateArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if args.Task == "" {
		return "", fmt.Errorf("task description is required")
	}

	taskID := fmt.Sprintf("task-%d", time.Now().UnixMilli())

	child, err := p.parentAgent.NewChildAgent(args.Task)
	if err != nil {
		return "", fmt.Errorf("create child agent: %w", err)
	}

	fullTask := buildFullTask(args)

	log.Info("dispatching async child agent",
		"task_id", taskID,
		"task_preview", truncate(args.Task, 100),
		"depth", child.Depth(),
	)

	go func() {
		start := time.Now()
		var result string
		var runErr error
		func() {
			defer func() {
				if r := recover(); r != nil {
					runErr = fmt.Errorf("child agent panicked: %v", r)
				}
			}()
			result, _, runErr = child.Run(context.Background(), fullTask)
		}()
		stats := child.GetStats()
		xml := buildTaskNotification(taskID, args.Task, result, runErr, time.Since(start), stats)
		p.parentAgent.AddNotification(xml)
		log.Info("async child agent finished",
			"task_id", taskID,
			"elapsed", time.Since(start).Round(time.Millisecond),
			"iterations", stats.TotalIterations,
			"tool_calls", stats.ToolCalls,
		)
	}()

	return fmt.Sprintf(
		`Task "%s" started (id: %s). Running in background — you will receive a <task-notification> when complete.`,
		truncate(args.Task, 80), taskID,
	), nil
}

// RegisterAsync registers the delegate_task_async tool with the global registry.
func (p *Provider) RegisterAsync() error {
	schema := types.ToolSchema{
		Type: "function",
		Function: types.FunctionSchema{
			Name: "delegate_task_async",
			Description: `Delegate a task to a sub-agent running in the background. Returns immediately with a task ID.
The sub-agent runs independently; you will receive a <task-notification> message when it completes.

Use this when:
- The task is independent and does not block your current reasoning
- You want to run multiple sub-tasks in parallel
- The task is long-running and you can continue responding to the user meanwhile

Use delegate_task (sync) instead when you need the result before taking your next step.

Constraints:
- Max delegation depth: 2 (cannot be called by a child agent)
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
						"description": "Additional context: file paths, variable values, background info the sub-agent needs"
					},
					"constraints": {
						"type": "string",
						"description": "Specific constraints or requirements: output format, scope boundaries"
					}
				},
				"required": ["task"],
				"additionalProperties": false
			}`),
		},
	}

	return registry.Register(&registry.ToolEntry{
		Name:          "delegate_task_async",
		Toolset:       "agent",
		Schema:        schema,
		Handler:       p.handleAsync,
		ParallelSafe:  true,
		NeverParallel: false,
		MaxResultSize: 200,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
