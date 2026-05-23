package state

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// TodoSnapshot 可序列化的 TODO 快照
type TodoSnapshot struct {
	SessionID string          `json:"session_id"`
	Items     json.RawMessage `json:"items"` // []TodoItem 的 JSON
	Version   int             `json:"version"`
}

// InitTodoSchema 初始化 TODO 持久化表（在 initSchema 之后调用）
func (s *SessionDB) InitTodoSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS session_todos (
    session_id TEXT NOT NULL REFERENCES sessions(id),
    items      TEXT NOT NULL,
    version    INTEGER NOT NULL DEFAULT 1,
    updated_at REAL NOT NULL,
    PRIMARY KEY (session_id)
);
`
	_, err := s.db.Exec(schema)
	return err
}

// SaveTodos 保存 TODO 快照（upsert）
func (s *SessionDB) SaveTodos(sessionID string, items json.RawMessage) error {
	return s.executeWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO session_todos (session_id, items, version, updated_at)
			VALUES (?, ?, 1, strftime('%s', 'now'))
			ON CONFLICT(session_id) DO UPDATE SET
				items = excluded.items,
				version = session_todos.version + 1,
				updated_at = strftime('%s', 'now')
		`, sessionID, string(items))
		if err != nil {
			return fmt.Errorf("save todos: %w", err)
		}
		log.Debug("todos persisted", "session_id", sessionID, "items_len", len(items))
		return nil
	})
}

// LoadTodos 加载 TODO 快照，不存在时返回 nil, nil
func (s *SessionDB) LoadTodos(sessionID string) (*TodoSnapshot, error) {
	row := s.db.QueryRow(
		`SELECT session_id, items, version FROM session_todos WHERE session_id = ?`,
		sessionID,
	)

	var snap TodoSnapshot
	var itemsStr string
	err := row.Scan(&snap.SessionID, &itemsStr, &snap.Version)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load todos: %w", err)
	}
	snap.Items = json.RawMessage(itemsStr)
	return &snap, nil
}

// DeleteTodos 删除 TODO 快照
func (s *SessionDB) DeleteTodos(sessionID string) error {
	return s.executeWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM session_todos WHERE session_id = ?`, sessionID)
		return err
	})
}
