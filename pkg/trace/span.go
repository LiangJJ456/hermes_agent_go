package trace

import (
	"context"
	"sync"
	"time"
)

// SpanStatus 表示 span 的终态
type SpanStatus int

const (
	SpanOK SpanStatus = iota
	SpanError
)

// Span 表示一个执行单元的生命周期
type Span struct {
	TraceID    string
	SpanID     string
	ParentID   string
	Name       string         // e.g. "node:llm_call", "agent_turn_3"
	NodeType   string         // llm, tool, human, choice, parallel, agent
	StartTime  time.Time
	EndTime    time.Time
	Attributes map[string]any // tokens_in, tokens_out, tool_name, model, etc.
	Status     SpanStatus
	Error      string
	mu         sync.Mutex
}

// StartSpan 创建子 span 并返回携带新 SpanContext 的 ctx
func StartSpan(ctx context.Context, name, nodeType string) (context.Context, *Span) {
	parent, _ := FromContext(ctx)
	traceID := parent.TraceID
	if traceID == "" {
		traceID = NewTraceID()
	}

	s := &Span{
		TraceID:    traceID,
		SpanID:     NewSpanID(),
		ParentID:   parent.SpanID,
		Name:       name,
		NodeType:   nodeType,
		StartTime:  time.Now(),
		Attributes: make(map[string]any),
	}

	newCtx := WithSpanContext(ctx, SpanContext{
		TraceID:  s.TraceID,
		SpanID:   s.SpanID,
		ParentID: s.ParentID,
	})
	return newCtx, s
}

// SetAttribute 设置 span 属性（线程安全）
func (s *Span) SetAttribute(k string, v any) {
	s.mu.Lock()
	s.Attributes[k] = v
	s.mu.Unlock()
}

// SetAttributes 批量设置属性
func (s *Span) SetAttributes(kvs map[string]any) {
	s.mu.Lock()
	for k, v := range kvs {
		s.Attributes[k] = v
	}
	s.mu.Unlock()
}

// End 结束 span
func (s *Span) End(err error) {
	s.EndTime = time.Now()
	if err != nil {
		s.Status = SpanError
		s.Error = err.Error()
	}
}

// Duration 返回 span 执行时长
func (s *Span) Duration() time.Duration {
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}
