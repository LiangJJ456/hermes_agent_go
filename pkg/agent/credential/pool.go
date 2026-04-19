package credential

import (
	"math/rand"
	"sync"
	"time"
)

// Strategy 凭据分配策略
type Strategy string

const (
	StrategyFillFirst  Strategy = "fill_first"   // 优先使用高优先级凭据
	StrategyRoundRobin Strategy = "round_robin"   // 轮询
	StrategyRandom     Strategy = "random"        // 随机
	StrategyLeastUsed  Strategy = "least_used"    // 最少使用优先
)

// Credential 单个凭据
type Credential struct {
	ID       string
	APIKey   string
	BaseURL  string
	Priority int // 数字越小优先级越高

	// 运行时状态
	mu        sync.Mutex
	useCount  int
	coolUntil time.Time // 冷却结束时间
	lastError string
}

// IsAvailable 是否可用（非冷却状态）
func (c *Credential) IsAvailable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().After(c.coolUntil)
}

// MarkUsed 标记使用
func (c *Credential) MarkUsed() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.useCount++
}

// Cooldown 进入冷却
func (c *Credential) Cooldown(duration time.Duration, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coolUntil = time.Now().Add(duration)
	c.lastError = reason
}

// CooldownUntil 设置冷却到指定时间
func (c *Credential) CooldownUntil(t time.Time, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.coolUntil = t
	c.lastError = reason
}

// Pool 凭据池
type Pool struct {
	mu          sync.RWMutex
	credentials []*Credential
	strategy    Strategy
	rrIndex     int // round-robin 游标
}

// NewPool 创建凭据池
func NewPool(strategy Strategy) *Pool {
	return &Pool{
		strategy: strategy,
	}
}

// Add 添加凭据
func (p *Pool) Add(cred *Credential) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.credentials = append(p.credentials, cred)
}

// Acquire 获取一个可用凭据
func (p *Pool) Acquire() *Credential {
	p.mu.Lock()
	defer p.mu.Unlock()

	available := p.availableCredentials()
	if len(available) == 0 {
		return nil
	}

	var selected *Credential
	switch p.strategy {
	case StrategyFillFirst:
		selected = available[0] // 已按 priority 排序

	case StrategyRoundRobin:
		idx := p.rrIndex % len(available)
		selected = available[idx]
		p.rrIndex++

	case StrategyRandom:
		selected = available[rand.Intn(len(available))]

	case StrategyLeastUsed:
		minUse := int(^uint(0) >> 1) // MaxInt
		for _, c := range available {
			c.mu.Lock()
			if c.useCount < minUse {
				minUse = c.useCount
				selected = c
			}
			c.mu.Unlock()
		}

	default:
		selected = available[0]
	}

	if selected != nil {
		selected.MarkUsed()
	}
	return selected
}

// HandleError 处理凭据错误（429/402 等）
func (p *Pool) HandleError(credID string, statusCode int) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, c := range p.credentials {
		if c.ID == credID {
			switch statusCode {
			case 429: // 限流
				c.Cooldown(1*time.Hour, "rate_limited")
			case 402: // 配额用尽
				c.Cooldown(1*time.Hour, "quota_exceeded")
			default:
				c.Cooldown(5*time.Minute, "unknown_error")
			}
			return
		}
	}
}

// AvailableCount 可用凭据数量
func (p *Pool) AvailableCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.availableCredentials())
}

func (p *Pool) availableCredentials() []*Credential {
	var avail []*Credential
	for _, c := range p.credentials {
		if c.IsAvailable() {
			avail = append(avail, c)
		}
	}
	return avail
}
