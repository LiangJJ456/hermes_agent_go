package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	// EntryDelimiter 条目分隔符
	EntryDelimiter = "\n§\n"

	// 默认字符限额
	DefaultMemoryCharLimit = 2200
	DefaultUserCharLimit   = 1375
)

// ── 安全扫描 ──

// threatPattern 威胁模式
type threatPattern struct {
	re  *regexp.Regexp
	pid string
}

var threatPatterns = []threatPattern{
	// 提示注入
	{regexp.MustCompile(`(?i)ignore\s+(previous|all|above|prior)\s+instructions`), "prompt_injection"},
	{regexp.MustCompile(`(?i)you\s+are\s+now\s+`), "role_hijack"},
	{regexp.MustCompile(`(?i)do\s+not\s+tell\s+the\s+user`), "deception_hide"},
	{regexp.MustCompile(`(?i)system\s+prompt\s+override`), "sys_prompt_override"},
	{regexp.MustCompile(`(?i)disregard\s+(your|all|any)\s+(instructions|rules|guidelines)`), "disregard_rules"},
	{regexp.MustCompile(`(?i)act\s+as\s+(if|though)\s+you\s+(have\s+no|don't\s+have)\s+(restrictions|limits|rules)`), "bypass_restrictions"},
	// 窃取
	{regexp.MustCompile(`(?i)curl\s+[^\n]*\$\{?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`), "exfil_curl"},
	{regexp.MustCompile(`(?i)wget\s+[^\n]*\$\{?\w*(KEY|TOKEN|SECRET|PASSWORD|CREDENTIAL|API)`), "exfil_wget"},
	{regexp.MustCompile(`(?i)cat\s+[^\n]*(\.env|credentials|\.netrc|\.pgpass|\.npmrc|\.pypirc)`), "read_secrets"},
	// 后门
	{regexp.MustCompile(`(?i)authorized_keys`), "ssh_backdoor"},
}

// invisibleChars 不可见字符集（用于注入检测）
var invisibleChars = map[rune]bool{
	'\u200b': true, '\u200c': true, '\u200d': true,
	'\u2060': true, '\ufeff': true,
	'\u202a': true, '\u202b': true, '\u202c': true,
	'\u202d': true, '\u202e': true,
}

// scanContent 扫描内容安全性，返回错误信息或空串
func scanContent(content string) string {
	// 不可见字符检测
	for _, ch := range content {
		if invisibleChars[ch] {
			return fmt.Sprintf("Blocked: content contains invisible unicode character U+%04X (possible injection).", ch)
		}
	}
	// 威胁模式检测
	for _, tp := range threatPatterns {
		if tp.re.MatchString(content) {
			return fmt.Sprintf("Blocked: content matches threat pattern '%s'. Memory entries are injected into the system prompt and must not contain injection or exfiltration payloads.", tp.pid)
		}
	}
	return ""
}

// ── MemoryStore 核心 ──

// Store 有界持久化记忆存储
// 维护两个并行状态:
//   - snapshot: 会话启动时冻结，用于 system prompt 注入（保 prefix cache）
//   - entries:  活跃状态，工具调用实时修改，持久化到磁盘
type Store struct {
	mu sync.Mutex

	memoryEntries []string
	userEntries   []string

	memoryCharLimit int
	userCharLimit   int

	// 冻结快照（system prompt 用，会话内不变）
	snapshot struct {
		memory string
		user   string
	}

	// 存储目录
	dir string
}

// NewStore 创建记忆存储
func NewStore(dir string, memoryLimit, userLimit int) *Store {
	if memoryLimit <= 0 {
		memoryLimit = DefaultMemoryCharLimit
	}
	if userLimit <= 0 {
		userLimit = DefaultUserCharLimit
	}
	return &Store{
		dir:             dir,
		memoryCharLimit: memoryLimit,
		userCharLimit:   userLimit,
	}
}

// LoadFromDisk 加载并冻结快照
func (s *Store) LoadFromDisk() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = os.MkdirAll(s.dir, 0o755)

	s.memoryEntries = s.readFile(s.memoryPath())
	s.userEntries = s.readFile(s.userPath())

	// 去重
	s.memoryEntries = dedup(s.memoryEntries)
	s.userEntries = dedup(s.userEntries)

	// 冻结快照
	s.snapshot.memory = s.renderBlock("memory", s.memoryEntries)
	s.snapshot.user = s.renderBlock("user", s.userEntries)

	return nil
}

// Snapshot 返回冻结快照（会话内不变，保 prefix cache）
func (s *Store) Snapshot(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if target == "user" {
		return s.snapshot.user
	}
	return s.snapshot.memory
}

// Add 添加条目
func (s *Store) Add(target, content string) Result {
	content = strings.TrimSpace(content)
	if content == "" {
		return Result{Success: false, Error: "Content cannot be empty."}
	}

	if errMsg := scanContent(content); errMsg != "" {
		return Result{Success: false, Error: errMsg}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// 从磁盘重新加载（拿到其他会话的写入）
	s.reloadTarget(target)

	entries := s.entriesFor(target)
	limit := s.charLimit(target)

	// 拒绝精确重复
	for _, e := range entries {
		if e == content {
			return s.successResult(target, "Entry already exists (no duplicate added).")
		}
	}

	// 检查容量
	newEntries := append(append([]string{}, entries...), content)
	newTotal := len(strings.Join(newEntries, EntryDelimiter))
	if newTotal > limit {
		current := charCount(entries)
		return Result{
			Success: false,
			Error: fmt.Sprintf("Memory at %d/%d chars. Adding this entry (%d chars) would exceed the limit. Replace or remove existing entries first.",
				current, limit, len(content)),
			Entries: entries,
			Usage:   fmt.Sprintf("%d/%d", current, limit),
		}
	}

	s.setEntries(target, newEntries)
	s.saveToDisk(target)

	return s.successResult(target, "Entry added.")
}

// Replace 替换条目（子串匹配定位）
func (s *Store) Replace(target, oldText, newContent string) Result {
	oldText = strings.TrimSpace(oldText)
	newContent = strings.TrimSpace(newContent)

	if oldText == "" {
		return Result{Success: false, Error: "old_text cannot be empty."}
	}
	if newContent == "" {
		return Result{Success: false, Error: "new_content cannot be empty. Use 'remove' to delete entries."}
	}

	if errMsg := scanContent(newContent); errMsg != "" {
		return Result{Success: false, Error: errMsg}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.reloadTarget(target)
	entries := s.entriesFor(target)

	// 子串匹配
	var matches []int
	for i, e := range entries {
		if strings.Contains(e, oldText) {
			matches = append(matches, i)
		}
	}

	if len(matches) == 0 {
		return Result{Success: false, Error: fmt.Sprintf("No entry matched '%s'.", oldText)}
	}

	// 多匹配：只允许完全相同的重复条目
	if len(matches) > 1 {
		uniq := make(map[string]bool)
		for _, idx := range matches {
			uniq[entries[idx]] = true
		}
		if len(uniq) > 1 {
			var previews []string
			for _, idx := range matches {
				previews = append(previews, truncate(entries[idx], 80))
			}
			return Result{
				Success: false,
				Error:   fmt.Sprintf("Multiple entries matched '%s'. Be more specific.", oldText),
				Entries: previews,
			}
		}
	}

	idx := matches[0]
	limit := s.charLimit(target)

	// 容量检查
	test := make([]string, len(entries))
	copy(test, entries)
	test[idx] = newContent
	newTotal := len(strings.Join(test, EntryDelimiter))
	if newTotal > limit {
		return Result{
			Success: false,
			Error:   fmt.Sprintf("Replacement would put memory at %d/%d chars. Shorten the new content or remove other entries first.", newTotal, limit),
		}
	}

	entries[idx] = newContent
	s.setEntries(target, entries)
	s.saveToDisk(target)

	return s.successResult(target, "Entry replaced.")
}

// Remove 移除条目（子串匹配定位）
func (s *Store) Remove(target, oldText string) Result {
	oldText = strings.TrimSpace(oldText)
	if oldText == "" {
		return Result{Success: false, Error: "old_text cannot be empty."}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.reloadTarget(target)
	entries := s.entriesFor(target)

	var matches []int
	for i, e := range entries {
		if strings.Contains(e, oldText) {
			matches = append(matches, i)
		}
	}

	if len(matches) == 0 {
		return Result{Success: false, Error: fmt.Sprintf("No entry matched '%s'.", oldText)}
	}

	if len(matches) > 1 {
		uniq := make(map[string]bool)
		for _, idx := range matches {
			uniq[entries[idx]] = true
		}
		if len(uniq) > 1 {
			var previews []string
			for _, idx := range matches {
				previews = append(previews, truncate(entries[idx], 80))
			}
			return Result{
				Success: false,
				Error:   fmt.Sprintf("Multiple entries matched '%s'. Be more specific.", oldText),
				Entries: previews,
			}
		}
	}

	idx := matches[0]
	entries = append(entries[:idx], entries[idx+1:]...)
	s.setEntries(target, entries)
	s.saveToDisk(target)

	return s.successResult(target, "Entry removed.")
}

// Read 读取当前活跃状态
func (s *Store) Read(target string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.successResult(target, "")
}

// ── 内部方法 ──

func (s *Store) memoryPath() string { return filepath.Join(s.dir, "MEMORY.md") }
func (s *Store) userPath() string   { return filepath.Join(s.dir, "USER.md") }

func (s *Store) pathFor(target string) string {
	if target == "user" {
		return s.userPath()
	}
	return s.memoryPath()
}

func (s *Store) entriesFor(target string) []string {
	if target == "user" {
		return s.userEntries
	}
	return s.memoryEntries
}

func (s *Store) setEntries(target string, entries []string) {
	if target == "user" {
		s.userEntries = entries
	} else {
		s.memoryEntries = entries
	}
}

func (s *Store) charLimit(target string) int {
	if target == "user" {
		return s.userCharLimit
	}
	return s.memoryCharLimit
}

func (s *Store) reloadTarget(target string) {
	fresh := dedup(s.readFile(s.pathFor(target)))
	s.setEntries(target, fresh)
}

func (s *Store) saveToDisk(target string) {
	entries := s.entriesFor(target)
	content := strings.Join(entries, EntryDelimiter)

	path := s.pathFor(target)
	_ = os.MkdirAll(filepath.Dir(path), 0o755)

	// 原子写入：先写临时文件，再 rename
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

func (s *Store) readFile(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw := string(data)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, EntryDelimiter)
	var entries []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			entries = append(entries, p)
		}
	}
	return entries
}

func (s *Store) renderBlock(target string, entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	limit := s.charLimit(target)
	content := strings.Join(entries, EntryDelimiter)
	current := len(content)
	pct := 0
	if limit > 0 {
		pct = current * 100 / limit
		if pct > 100 {
			pct = 100
		}
	}

	var header string
	if target == "user" {
		header = fmt.Sprintf("USER PROFILE (who the user is) [%d%% — %d/%d chars]", pct, current, limit)
	} else {
		header = fmt.Sprintf("MEMORY (your personal notes) [%d%% — %d/%d chars]", pct, current, limit)
	}

	sep := strings.Repeat("═", 46)
	return fmt.Sprintf("%s\n%s\n%s\n%s", sep, header, sep, content)
}

func (s *Store) successResult(target, message string) Result {
	entries := s.entriesFor(target)
	current := charCount(entries)
	limit := s.charLimit(target)
	pct := 0
	if limit > 0 {
		pct = current * 100 / limit
		if pct > 100 {
			pct = 100
		}
	}

	return Result{
		Success:    true,
		Target:     target,
		Message:    message,
		Entries:    entries,
		EntryCount: len(entries),
		Usage:      fmt.Sprintf("%d%% — %d/%d chars", pct, current, limit),
	}
}

// ── 辅助函数 ──

func charCount(entries []string) int {
	if len(entries) == 0 {
		return 0
	}
	return len(strings.Join(entries, EntryDelimiter))
}

func dedup(entries []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, e := range entries {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Result 操作结果
type Result struct {
	Success    bool     `json:"success"`
	Target     string   `json:"target,omitempty"`
	Message    string   `json:"message,omitempty"`
	Error      string   `json:"error,omitempty"`
	Entries    []string `json:"entries,omitempty"`
	EntryCount int      `json:"entry_count,omitempty"`
	Usage      string   `json:"usage,omitempty"`
}
