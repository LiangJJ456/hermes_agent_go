package context

import "context"

// Message is a single conversation turn.
type Message struct {
	Role    string `json:"Role"`
	Content string `json:"Content"`
	Name    string `json:"Name,omitempty"`
}

// ConversationMemory holds session-scoped message history.
type ConversationMemory struct {
	SessionID string                 `json:"SessionID"`
	Messages  []Message              `json:"Messages"`
	Metadata  map[string]interface{} `json:"Metadata,omitempty"`
}

// AddMessage appends a message to the conversation.
func (cm *ConversationMemory) AddMessage(msg Message) {
	cm.Messages = append(cm.Messages, msg)
}

// MemoryStore persists conversation memory across sessions.
type MemoryStore interface {
	SaveSession(ctx context.Context, session *ConversationMemory) error
	LoadSession(ctx context.Context, sessionID string) (*ConversationMemory, error)
	DeleteSession(ctx context.Context, sessionID string) error
}
