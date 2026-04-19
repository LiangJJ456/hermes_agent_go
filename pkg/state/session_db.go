package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/errx"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	schemaVersion       = 1
	writeMaxRetries     = 15
	writeRetryMinMs     = 20
	writeRetryMaxMs     = 150
	checkpointEveryN    = 50
)

// SessionDB SQLite 会话存储（线程安全）
type SessionDB struct {
	db         *sql.DB
	mu         sync.Mutex
	writeCount atomic.Int64
}

// Session 会话元数据
type Session struct {
	ID              string    `json:"id"`
	Source          string    `json:"source"`
	UserID          string    `json:"user_id,omitempty"`
	Model           string    `json:"model,omitempty"`
	SystemPrompt    string    `json:"system_prompt,omitempty"`
	ParentSessionID string    `json:"parent_session_id,omitempty"`
	StartedAt       time.Time `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty"`
	EndReason       string    `json:"end_reason,omitempty"`
	MessageCount    int       `json:"message_count"`
	ToolCallCount   int       `json:"tool_call_count"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	Title           string    `json:"title,omitempty"`
}

// NewSessionDB 打开/创建数据库
func NewSessionDB(dbPath string) (*SessionDB, error) {
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=1000", dbPath))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// WAL mode + 连接池配置
	db.SetMaxOpenConns(1) // SQLite 写入串行
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // 不过期

	sdb := &SessionDB{db: db}
	if err := sdb.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	return sdb, nil
}

func (s *SessionDB) initSchema() error {
	schema := `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    user_id TEXT,
    model TEXT,
    system_prompt TEXT,
    parent_session_id TEXT,
    started_at REAL NOT NULL,
    ended_at REAL,
    end_reason TEXT,
    message_count INTEGER DEFAULT 0,
    tool_call_count INTEGER DEFAULT 0,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    title TEXT,
    FOREIGN KEY (parent_session_id) REFERENCES sessions(id)
);

CREATE TABLE IF NOT EXISTS messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id),
    role TEXT NOT NULL,
    content TEXT,
    tool_call_id TEXT,
    tool_calls TEXT,
    tool_name TEXT,
    timestamp REAL NOT NULL,
    token_count INTEGER,
    finish_reason TEXT,
    reasoning TEXT
);

CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, timestamp);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    content,
    content=messages,
    content_rowid=id
);

CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
`
	_, err := s.db.Exec(schema)
	return err
}

// executeWrite 带 jitter 退避的写入事务
func (s *SessionDB) executeWrite(fn func(tx *sql.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt < writeMaxRetries; attempt++ {
		s.mu.Lock()
		tx, err := s.db.Begin()
		if err != nil {
			s.mu.Unlock()
			return fmt.Errorf("begin tx: %w", err)
		}

		if err := fn(tx); err != nil {
			tx.Rollback()
			s.mu.Unlock()
			// 检查是否是锁竞争
			if isLockedErr(err) {
				lastErr = err
				jitter := time.Duration(writeRetryMinMs+rand.Intn(writeRetryMaxMs-writeRetryMinMs)) * time.Millisecond
				time.Sleep(jitter)
				continue
			}
			return err
		}

		if err := tx.Commit(); err != nil {
			s.mu.Unlock()
			if isLockedErr(err) {
				lastErr = err
				jitter := time.Duration(writeRetryMinMs+rand.Intn(writeRetryMaxMs-writeRetryMinMs)) * time.Millisecond
				time.Sleep(jitter)
				continue
			}
			return fmt.Errorf("commit: %w", err)
		}
		s.mu.Unlock()

		// 成功 — 周期性 checkpoint
		n := s.writeCount.Add(1)
		if n%checkpointEveryN == 0 {
			s.tryCheckpoint()
		}
		return nil
	}
	return fmt.Errorf("%w: %v", errx.ErrDBLocked, lastErr)
}

// CreateSession 创建新会话
func (s *SessionDB) CreateSession(session *Session) error {
	return s.executeWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO sessions (id, source, user_id, model, system_prompt, parent_session_id, started_at, title)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			session.ID, session.Source, session.UserID, session.Model,
			session.SystemPrompt, session.ParentSessionID,
			float64(session.StartedAt.UnixMilli())/1000.0, session.Title,
		)
		return err
	})
}

// SaveMessage 保存消息
func (s *SessionDB) SaveMessage(sessionID string, msg *types.Message) error {
	return s.executeWrite(func(tx *sql.Tx) error {
		toolCallsJSON := ""
		if len(msg.ToolCalls) > 0 {
			b, _ := json.Marshal(msg.ToolCalls)
			toolCallsJSON = string(b)
		}

		_, err := tx.Exec(
			`INSERT INTO messages (session_id, role, content, tool_call_id, tool_calls, tool_name, timestamp, reasoning)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			sessionID, string(msg.Role), msg.Content, msg.ToolCallID,
			toolCallsJSON, msg.Name,
			float64(msg.Timestamp.UnixMilli())/1000.0, msg.Reasoning,
		)
		if err != nil {
			return err
		}

		// 更新会话计数
		_, err = tx.Exec(
			`UPDATE sessions SET message_count = message_count + 1 WHERE id = ?`,
			sessionID,
		)
		return err
	})
}

// SearchMessages FTS5 全文搜索
func (s *SessionDB) SearchMessages(query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT m.session_id, m.role, m.content, m.timestamp,
		        highlight(messages_fts, 0, '<mark>', '</mark>') as snippet
		 FROM messages_fts
		 JOIN messages m ON messages_fts.rowid = m.id
		 WHERE messages_fts MATCH ?
		 ORDER BY rank
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var ts float64
		if err := rows.Scan(&r.SessionID, &r.Role, &r.Content, &ts, &r.Snippet); err != nil {
			continue
		}
		r.Timestamp = time.Unix(int64(ts), int64((ts-float64(int64(ts)))*1e9))
		results = append(results, r)
	}
	return results, nil
}

// SearchResult 搜索结果
type SearchResult struct {
	SessionID string
	Role      string
	Content   string
	Snippet   string
	Timestamp time.Time
}

// GetSession 获取会话
func (s *SessionDB) GetSession(id string) (*Session, error) {
	row := s.db.QueryRow(
		`SELECT id, source, user_id, model, parent_session_id, started_at, ended_at,
		        end_reason, message_count, tool_call_count, input_tokens, output_tokens, title
		 FROM sessions WHERE id = ?`, id,
	)

	var sess Session
	var startedAt, endedAt sql.NullFloat64
	var userID, model, parentID, endReason, title sql.NullString

	err := row.Scan(
		&sess.ID, &sess.Source, &userID, &model, &parentID,
		&startedAt, &endedAt, &endReason,
		&sess.MessageCount, &sess.ToolCallCount,
		&sess.InputTokens, &sess.OutputTokens, &title,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errx.ErrSessionNotFound
		}
		return nil, err
	}

	if userID.Valid {
		sess.UserID = userID.String
	}
	if model.Valid {
		sess.Model = model.String
	}
	if parentID.Valid {
		sess.ParentSessionID = parentID.String
	}
	if endReason.Valid {
		sess.EndReason = endReason.String
	}
	if title.Valid {
		sess.Title = title.String
	}
	if startedAt.Valid {
		sess.StartedAt = time.Unix(int64(startedAt.Float64), 0)
	}
	if endedAt.Valid {
		t := time.Unix(int64(endedAt.Float64), 0)
		sess.EndedAt = &t
	}

	return &sess, nil
}

// ListSessions 列出会话
func (s *SessionDB) ListSessions(limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		`SELECT id, source, user_id, model, started_at, message_count, tool_call_count, title
		 FROM sessions ORDER BY started_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var sess Session
		var startedAt float64
		var userID, model, title sql.NullString
		if err := rows.Scan(&sess.ID, &sess.Source, &userID, &model, &startedAt,
			&sess.MessageCount, &sess.ToolCallCount, &title); err != nil {
			continue
		}
		sess.StartedAt = time.Unix(int64(startedAt), 0)
		if userID.Valid {
			sess.UserID = userID.String
		}
		if model.Valid {
			sess.Model = model.String
		}
		if title.Valid {
			sess.Title = title.String
		}
		sessions = append(sessions, &sess)
	}
	return sessions, nil
}

// Close 关闭数据库
func (s *SessionDB) Close() error {
	s.tryCheckpoint()
	return s.db.Close()
}

func (s *SessionDB) tryCheckpoint() {
	_, err := s.db.Exec("PRAGMA wal_checkpoint(PASSIVE)")
	if err != nil {
		log.Debug("wal checkpoint failed", "error", err)
	}
}

func isLockedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "locked") || contains(msg, "busy")
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsImpl(s, sub))
}

func containsImpl(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
