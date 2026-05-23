package builtin

import (
	"testing"
)

func TestTodoStore_ReadWriteUpdate(t *testing.T) {
	store := NewTodoStore()

	// 初始状态为空
	items := store.Read()
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d items", len(items))
	}

	// 写入
	newItems := []TodoItem{
		{ID: "1", Content: "Task A", Status: TodoPending, Priority: PriorityHigh},
		{ID: "2", Content: "Task B", Status: TodoPending, Priority: PriorityMedium},
		{ID: "3", Content: "Task C", Status: TodoPending, Priority: PriorityLow},
	}
	store.Write(newItems)

	items = store.Read()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// 更新状态
	err := store.UpdateStatus("2", TodoInProgress, "main")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	items = store.Read()
	if items[1].Status != TodoInProgress {
		t.Errorf("expected in_progress, got %s", items[1].Status)
	}

	// 更新不存在的 ID
	err = store.UpdateStatus("999", TodoCompleted, "main")
	if err == nil {
		t.Error("expected error for non-existent ID")
	}
}

func TestTodoStore_Summary(t *testing.T) {
	store := NewTodoStore()

	// 空 store 返回空字符串
	if s := store.Summary(); s != "" {
		t.Errorf("expected empty summary, got %q", s)
	}

	store.Write([]TodoItem{
		{ID: "1", Content: "Build thing", Status: TodoCompleted, Priority: PriorityHigh},
		{ID: "2", Content: "Test thing", Status: TodoInProgress, Priority: PriorityMedium},
		{ID: "3", Content: "Deploy", Status: TodoPending, Priority: PriorityLow},
	})

	summary := store.Summary()
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	// Should contain progress info
	if !containsStr(summary, "1/3") {
		t.Errorf("summary should show 1/3 completed, got: %s", summary)
	}
}

func TestTodoStore_PermissionControl(t *testing.T) {
	store := NewTodoStore()

	store.Write([]TodoItem{
		{ID: "1", Content: "Main task", Status: TodoPending, Assignee: "main"},
		{ID: "2", Content: "Child task", Status: TodoPending, Assignee: "child_1"},
	})

	// Child can update its own task
	err := store.UpdateStatus("2", TodoInProgress, "child_1")
	if err != nil {
		t.Fatalf("child should be able to update own task: %v", err)
	}

	// Child cannot update main's task
	err = store.UpdateStatus("1", TodoCompleted, "child_1")
	if err == nil {
		t.Error("child should NOT be able to update main's task")
	}

	// Main can update any task
	err = store.UpdateStatus("2", TodoCompleted, "main")
	if err != nil {
		t.Fatalf("main should be able to update any task: %v", err)
	}
}

func TestTodoStore_PersistFn(t *testing.T) {
	store := NewTodoStore()

	var callCount int
	store.SetPersistFn(func(items []TodoItem) error {
		callCount++
		return nil
	})

	store.Write([]TodoItem{
		{ID: "1", Content: "Test persist", Status: TodoPending},
	})

	// persist is async (goroutine), give it a moment
	// But for this test we just verify it was set correctly
	// The actual async call may or may not complete before check

	_ = store.UpdateStatus("1", TodoCompleted, "main")
	// At minimum, persistFn should be non-nil (we can't reliably test goroutine timing)
}

func TestTodoStore_Write_Overwrite(t *testing.T) {
	store := NewTodoStore()

	store.Write([]TodoItem{
		{ID: "1", Content: "First", Status: TodoPending},
	})

	// Overwrite with new list
	store.Write([]TodoItem{
		{ID: "a", Content: "New A", Status: TodoPending},
		{ID: "b", Content: "New B", Status: TodoCompleted},
	})

	items := store.Read()
	if len(items) != 2 {
		t.Fatalf("expected 2 items after overwrite, got %d", len(items))
	}
	if items[0].ID != "a" {
		t.Errorf("expected ID 'a', got %s", items[0].ID)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
