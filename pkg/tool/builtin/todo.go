package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// ── TODO 数据结构 ──

// TodoStatus 任务状态
type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
	TodoBlocked    TodoStatus = "blocked"
)

// TodoPriority 优先级
type TodoPriority string

const (
	PriorityHigh   TodoPriority = "high"
	PriorityMedium TodoPriority = "medium"
	PriorityLow    TodoPriority = "low"
)

// TodoItem 单个任务项
type TodoItem struct {
	ID       string       `json:"id"`
	Content  string       `json:"content"`
	Status   TodoStatus   `json:"status"`
	Priority TodoPriority `json:"priority,omitempty"`
	Assignee string       `json:"assignee,omitempty"` // "main" 或子 agent session_id
}

// ── TodoStore 线程安全的全局 TODO 存储 ──

// PersistFn 持久化回调（由外部注入，用于写入 session_db）
type PersistFn func(items []TodoItem) error

// TodoStore 全局 TODO 状态（主 agent 和子 agent 共享同一实例）
type TodoStore struct {
	mu        sync.RWMutex
	items     []TodoItem
	persistFn PersistFn // 可选：持久化回调
}

// NewTodoStore 创建 TODO 存储
func NewTodoStore() *TodoStore {
	return &TodoStore{}
}

// SetPersistFn 设置持久化回调（每次 TODO 变更后自动调用）
func (s *TodoStore) SetPersistFn(fn PersistFn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistFn = fn
}

// persist 内部持久化触发（需在持有写锁时调用后释放锁再执行，或异步）
func (s *TodoStore) persist() {
	if s.persistFn == nil {
		return
	}
	// 复制一份避免持锁调用外部函数
	itemsCopy := make([]TodoItem, len(s.items))
	copy(itemsCopy, s.items)
	go func() {
		if err := s.persistFn(itemsCopy); err != nil {
			// 持久化失败不阻断主流程，仅记录
			_ = err
		}
	}()
}

// LoadFrom 从持久化快照恢复 TODO 状态（用于断点续跑）
func (s *TodoStore) LoadFrom(items []TodoItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = make([]TodoItem, len(items))
	copy(s.items, items)
}

// Write 全量覆盖 TODO list（仅主 agent 可调用）
func (s *TodoStore) Write(items []TodoItem) {
	s.mu.Lock()
	s.items = make([]TodoItem, len(items))
	copy(s.items, items)
	s.mu.Unlock()
	s.persist()
}

// UpdateStatus 更新指定 ID 的状态（子 agent 只能更新 assignee == 自己的项）
func (s *TodoStore) UpdateStatus(id string, status TodoStatus, callerID string) error {
	s.mu.Lock()
	for i := range s.items {
		if s.items[i].ID == id {
			// 子 agent 权限检查：只能更新分配给自己的任务
			if callerID != "main" && s.items[i].Assignee != "" && s.items[i].Assignee != callerID {
				s.mu.Unlock()
				return fmt.Errorf("permission denied: task %s is assigned to %s, not %s", id, s.items[i].Assignee, callerID)
			}
			s.items[i].Status = status
			s.mu.Unlock()
			s.persist()
			return nil
		}
	}
	s.mu.Unlock()
	return fmt.Errorf("todo item %s not found", id)
}

// Read 读取当前 TODO list 快照
func (s *TodoStore) Read() []TodoItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TodoItem, len(s.items))
	copy(out, s.items)
	return out
}

// Summary 生成简洁的状态摘要文本（用于 system reminder 注入）
func (s *TodoStore) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.items) == 0 {
		return ""
	}

	var sb strings.Builder
	pending, inProgress, completed, blocked := 0, 0, 0, 0
	for _, item := range s.items {
		switch item.Status {
		case TodoPending:
			pending++
		case TodoInProgress:
			inProgress++
		case TodoCompleted:
			completed++
		case TodoBlocked:
			blocked++
		}
	}
	sb.WriteString(fmt.Sprintf("Progress: %d/%d completed", completed, len(s.items)))
	if blocked > 0 {
		sb.WriteString(fmt.Sprintf(", %d blocked", blocked))
	}
	sb.WriteString("\n")

	for _, item := range s.items {
		icon := "○"
		switch item.Status {
		case TodoCompleted:
			icon = "✓"
		case TodoInProgress:
			icon = "▶"
		case TodoBlocked:
			icon = "✗"
		}
		line := fmt.Sprintf("  %s [%s] %s", icon, item.ID, item.Content)
		if item.Assignee != "" && item.Assignee != "main" {
			line += fmt.Sprintf(" (@%s)", item.Assignee)
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// ── 工具注册 ──

// todoWriteArgs todo_write 工具参数
type todoWriteArgs struct {
	Todos []TodoItem `json:"todos"`
}

// todoUpdateArgs todo_update 工具参数
type todoUpdateArgs struct {
	ID     string     `json:"id"`
	Status TodoStatus `json:"status"`
}

// todoReadArgs todo_read 工具参数（无参数）
type todoReadArgs struct{}

// RegisterTodoTools 注册 TODO 相关工具
// callerID: "main" 表示主 agent，其他值为子 agent 的 session ID
func RegisterTodoTools(store *TodoStore, callerID string) error {
	// ── todo_read：所有 agent 都可读 ──
	readSchema := types.ToolSchema{
		Type: "function",
		Function: types.FunctionSchema{
			Name: "todo_read",
			Description: `Read the current TODO task list. Returns all tasks with their status.
Use this to check progress, find the next task to work on, or understand the overall plan.`,
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {},
				"additionalProperties": false
			}`),
		},
	}

	registry.RegisterOrReplace(&registry.ToolEntry{
		Name:         "todo_read",
		Toolset:      "planning",
		Schema:       readSchema,
		ParallelSafe: true,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			items := store.Read()
			if len(items) == 0 {
				return "TODO list is empty. Consider creating a plan with todo_write if working on a multi-step task.", nil
			}
			data, _ := json.MarshalIndent(items, "", "  ")
			return string(data) + "\n\n---\nContinue working on the next pending or in_progress task.", nil
		},
	})

	// ── todo_write：仅主 agent 可用（全量覆盖） ──
	if callerID == "main" {
		writeSchema := types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "todo_write",
				Description: `Create or fully replace the TODO task list. Use this at the start of complex tasks to plan your work, or to update the plan when circumstances change.

Each todo item has: id (short string), content (task description), status (pending/in_progress/completed/blocked), priority (high/medium/low), assignee (optional, for sub-agent delegation).

IMPORTANT: This overwrites the entire list. Include ALL items (completed ones too) when updating.`,
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"todos": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"id": {"type": "string", "description": "Short unique ID, e.g. '1', '2a'"},
									"content": {"type": "string", "description": "Task description"},
									"status": {"type": "string", "enum": ["pending", "in_progress", "completed", "blocked"]},
									"priority": {"type": "string", "enum": ["high", "medium", "low"]},
									"assignee": {"type": "string", "description": "Who owns this task: 'main' or a sub-agent ID"}
								},
								"required": ["id", "content", "status"]
							},
							"description": "Complete list of all TODO items"
						}
					},
					"required": ["todos"],
					"additionalProperties": false
				}`),
			},
		}

		registry.RegisterOrReplace(&registry.ToolEntry{
			Name:         "todo_write",
			Toolset:      "planning",
			Schema:       writeSchema,
			ParallelSafe: false,
			Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var a todoWriteArgs
				if err := json.Unmarshal(args, &a); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
				if len(a.Todos) == 0 {
					return "", fmt.Errorf("todos list cannot be empty")
				}
				// 验证 status
				for _, t := range a.Todos {
					switch t.Status {
					case TodoPending, TodoInProgress, TodoCompleted, TodoBlocked:
					default:
						return "", fmt.Errorf("invalid status %q for item %s", t.Status, t.ID)
					}
				}
				store.Write(a.Todos)
				summary := store.Summary()
				return fmt.Sprintf("TODO list updated (%d items).\n\n%s\n---\nNow proceed with the next pending task. Mark it as in_progress before starting work.", len(a.Todos), summary), nil
			},
		})
	}

	// ── todo_update：所有 agent 可用（单项状态更新，带权限控制） ──
	updateSchema := types.ToolSchema{
		Type: "function",
		Function: types.FunctionSchema{
			Name: "todo_update",
			Description: `Update the status of a single TODO item. Use this to mark a task as in_progress when starting it, or completed when done.
Sub-agents can only update tasks assigned to them.`,
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"id": {"type": "string", "description": "The task ID to update"},
					"status": {"type": "string", "enum": ["pending", "in_progress", "completed", "blocked"]}
				},
				"required": ["id", "status"],
				"additionalProperties": false
			}`),
		},
	}

	registry.RegisterOrReplace(&registry.ToolEntry{
		Name:         "todo_update",
		Toolset:      "planning",
		Schema:       updateSchema,
		ParallelSafe: true,
		Handler: func(ctx context.Context, args json.RawMessage) (string, error) {
			var a todoUpdateArgs
			if err := json.Unmarshal(args, &a); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if err := store.UpdateStatus(a.ID, a.Status, callerID); err != nil {
				return "", err
			}
			summary := store.Summary()
			return fmt.Sprintf("Task %s → %s\n\n%s\n---\nContinue with the next task.", a.ID, a.Status, summary), nil
		},
	})

	return nil
}
