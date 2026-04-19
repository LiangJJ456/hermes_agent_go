package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// parallelSafeTools 只读工具，可以安全并行
var parallelSafeTools = map[string]bool{
	"read_file":     true,
	"search_files":  true,
	"web_search":    true,
	"web_extract":   true,
	"vision":        true,
	"skills_list":   true,
	"skill_view":    true,
	"memory_search": true,
}

// neverParallelTools 永远不能并行的工具
var neverParallelTools = map[string]bool{
	"clarify": true,
}

// pathScopedTools 路径作用域工具，路径不重叠时可并行
var pathScopedTools = map[string]bool{
	"read_file":  true,
	"write_file": true,
	"patch":      true,
}

// toolCallWithResult 工具调用+结果
type toolCallWithResult struct {
	Call   types.ToolCall
	Result types.ToolResult
}

// ParallelExecutor 并行工具执行器
type ParallelExecutor struct {
	registry    *registry.Registry
	maxWorkers  int
}

// NewParallelExecutor 创建执行器
func NewParallelExecutor(reg *registry.Registry, maxWorkers int) *ParallelExecutor {
	if maxWorkers <= 0 {
		maxWorkers = 8
	}
	return &ParallelExecutor{registry: reg, maxWorkers: maxWorkers}
}

// ShouldParallelize 判断一批工具调用是否可以并行
func (pe *ParallelExecutor) ShouldParallelize(calls []types.ToolCall) bool {
	if len(calls) <= 1 {
		return false
	}

	// 任一工具在 never-parallel 中 → 串行
	for _, c := range calls {
		if neverParallelTools[c.Function.Name] {
			return false
		}
	}

	// 检查路径作用域工具是否有路径重叠
	paths := make(map[string]bool)
	for _, c := range calls {
		name := c.Function.Name
		if pathScopedTools[name] {
			p := extractPathArg(c.Function.Arguments)
			if p != "" {
				absP, _ := filepath.Abs(p)
				if paths[absP] {
					return false // 路径重叠
				}
				paths[absP] = true
			}
		} else if !parallelSafeTools[name] {
			// 非只读、非路径作用域 → 串行
			return false
		}
	}
	return true
}

// ExecuteBatch 并行执行一批工具调用
func (pe *ParallelExecutor) ExecuteBatch(ctx context.Context, calls []types.ToolCall) []toolCallWithResult {
	results := make([]toolCallWithResult, len(calls))

	if !pe.ShouldParallelize(calls) {
		// 串行
		for i, c := range calls {
			result := pe.executeSingle(ctx, c)
			results[i] = toolCallWithResult{Call: c, Result: result}
		}
		return results
	}

	// 并行，限制最大并发
	sem := make(chan struct{}, pe.maxWorkers)
	var wg sync.WaitGroup

	for i, c := range calls {
		i, c := i, c
		wg.Add(1)
		sem <- struct{}{} // 获取信号量
		go func() {
			defer wg.Done()
			defer func() { <-sem }() // 释放信号量
			result := pe.executeSingle(ctx, c)
			results[i] = toolCallWithResult{Call: c, Result: result}
		}()
	}
	wg.Wait()
	return results
}

func (pe *ParallelExecutor) executeSingle(ctx context.Context, call types.ToolCall) types.ToolResult {
	name := call.Function.Name
	log.Debug("executing tool", "name", name, "call_id", call.ID)

	output, err := pe.registry.Execute(ctx, name, json.RawMessage(call.Function.Arguments))
	if err != nil {
		log.Warn("tool execution failed", "name", name, "error", err)
		return types.ToolResult{
			ToolCallID: call.ID,
			Content:    "Error: " + err.Error(),
			IsError:    true,
		}
	}

	return types.ToolResult{
		ToolCallID: call.ID,
		Content:    output,
	}
}

// extractPathArg 从工具参数 JSON 中提取 path/file_path 字段
func extractPathArg(argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filepath", "file"} {
		if v, ok := args[key]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}
