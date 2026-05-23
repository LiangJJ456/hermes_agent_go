package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/errx"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// Handler 工具处理函数
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// CheckFn 工具可用性检查函数，返回 true 表示可用
type CheckFn func() bool

// ToolEntry 注册表条目
type ToolEntry struct {
	Name             string
	Toolset          string // 所属工具集
	Schema           types.ToolSchema
	Handler          Handler
	CheckFn          CheckFn
	ParallelSafe     bool // 是否可以与其他工具并行
	NeverParallel    bool // 是否永远不并行（如 clarify）
	MaxResultSize    int  // 最大结果字符数，0 = 不限
}

// Registry 全局工具注册表（线程安全）
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*ToolEntry
	order []string // 保持注册顺序
}

var globalRegistry = &Registry{
	tools: make(map[string]*ToolEntry),
}

// Global 返回全局注册表单例
func Global() *Registry {
	return globalRegistry
}

// Register 注册工具
func (r *Registry) Register(entry *ToolEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tools[entry.Name]; exists {
		return fmt.Errorf("%w: %s", errx.ErrToolAlreadyExists, entry.Name)
	}

	// 确保 schema type 正确
	entry.Schema.Type = "function"
	entry.Schema.Function.Name = entry.Name

	r.tools[entry.Name] = entry
	r.order = append(r.order, entry.Name)
	log.Debug("tool registered", "name", entry.Name, "toolset", entry.Toolset)
	return nil
}

// Get 获取工具
func (r *Registry) Get(name string) (*ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Execute 执行工具
func (r *Registry) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	entry, ok := r.Get(name)
	if !ok {
		return "", fmt.Errorf("%w: %s", errx.ErrToolNotFound, name)
	}

	result, err := entry.Handler(ctx, args)
	if err != nil {
		return "", fmt.Errorf("%w: %s: %v", errx.ErrToolExecutionFailed, name, err)
	}

	// 截断过长结果
	if entry.MaxResultSize > 0 && len(result) > entry.MaxResultSize {
		result = result[:entry.MaxResultSize] + "\n... [truncated]"
	}

	return result, nil
}

// GetSchemas 获取指定工具集的 Schema 列表
func (r *Registry) GetSchemas(toolsets []string, disabled []string) []types.ToolSchema {
	r.mu.RLock()
	defer r.mu.RUnlock()

	disabledSet := make(map[string]struct{}, len(disabled))
	for _, d := range disabled {
		disabledSet[d] = struct{}{}
	}

	toolsetSet := make(map[string]struct{}, len(toolsets))
	for _, ts := range toolsets {
		toolsetSet[ts] = struct{}{}
	}

	var schemas []types.ToolSchema
	for _, name := range r.order {
		entry := r.tools[name]

		// 跳过禁用的工具
		if _, ok := disabledSet[name]; ok {
			continue
		}

		// 检查工具集过滤
		if len(toolsetSet) > 0 {
			if _, ok := toolsetSet[entry.Toolset]; !ok {
				continue
			}
		}

		// 检查可用性
		if entry.CheckFn != nil && !entry.CheckFn() {
			continue
		}

		schemas = append(schemas, entry.Schema)
	}
	return schemas
}

// ListNames 列出所有已注册工具名
func (r *Registry) ListNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// RegisterOrReplace 注册工具，如已存在则替换（用于子 agent 需要覆盖 handler 的场景）
func (r *Registry) RegisterOrReplace(entry *ToolEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry.Schema.Type = "function"
	entry.Schema.Function.Name = entry.Name

	if _, exists := r.tools[entry.Name]; !exists {
		r.order = append(r.order, entry.Name)
	}
	r.tools[entry.Name] = entry
}

// Count 已注册工具数量
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ── 快捷注册函数 ──

// Register 向全局注册表注册工具
func Register(entry *ToolEntry) error {
	return Global().Register(entry)
}

// RegisterOrReplace 向全局注册表注册工具（已存在则替换）
func RegisterOrReplace(entry *ToolEntry) {
	Global().RegisterOrReplace(entry)
}
