package agent

import (
	"sync"
	"sync/atomic"
)

// IterationBudget 线程安全的迭代预算计数器
// 每个 Agent（包括子 Agent）拥有独立的预算
type IterationBudget struct {
	max       int
	used      atomic.Int64
	mu        sync.Mutex
	onExhaust func() // 预算耗尽回调
}

// NewIterationBudget 创建预算
func NewIterationBudget(max int) *IterationBudget {
	return &IterationBudget{max: max}
}

// SetOnExhaust 设置预算耗尽回调
func (b *IterationBudget) SetOnExhaust(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onExhaust = fn
}

// Consume 消耗 1 次迭代，返回是否还有剩余
func (b *IterationBudget) Consume() bool {
	newVal := b.used.Add(1)
	if int(newVal) > b.max {
		b.used.Add(-1) // 回退
		b.mu.Lock()
		cb := b.onExhaust
		b.mu.Unlock()
		if cb != nil {
			cb()
		}
		return false
	}
	return true
}

// Refund 退还迭代次数（如 execute_code 不占预算）
func (b *IterationBudget) Refund(n int) {
	b.used.Add(-int64(n))
}

// Remaining 剩余次数
func (b *IterationBudget) Remaining() int {
	r := b.max - int(b.used.Load())
	if r < 0 {
		return 0
	}
	return r
}

// Used 已使用次数
func (b *IterationBudget) Used() int {
	return int(b.used.Load())
}

// Max 最大次数
func (b *IterationBudget) Max() int {
	return b.max
}
