package context

import (
	"context"
	"sync"
)

// Message is a single conversation turn.
type Message struct {
	Role       string                   `json:"Role"`
	Content    string                   `json:"Content"`
	Name       string                   `json:"Name,omitempty"`
	ToolCalls  []map[string]interface{} `json:"ToolCalls,omitempty"`
	ToolCallID string                   `json:"ToolCallID,omitempty"`
}

// ConversationMemory holds session-scoped message history.
type ConversationMemory struct {
	SessionID string                 `json:"SessionID"`
	Messages  []Message              `json:"Messages"`
	Metadata  map[string]interface{} `json:"Metadata,omitempty"`
	mu        sync.Mutex
}

// AddMessage appends a message to the conversation (thread-safe).
func (cm *ConversationMemory) AddMessage(msg Message) {
	cm.mu.Lock()
	cm.Messages = append(cm.Messages, msg)
	cm.mu.Unlock()
}

// MemoryStore persists conversation memory across sessions.
type MemoryStore interface {
	SaveSession(ctx context.Context, session *ConversationMemory) error
	LoadSession(ctx context.Context, sessionID string) (*ConversationMemory, error)
	DeleteSession(ctx context.Context, sessionID string) error
}
