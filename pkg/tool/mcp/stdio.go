package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// StdioTransport communicates with an MCP server via stdin/stdout of a subprocess.
type StdioTransport struct {
	command string
	args    []string
	env     map[string]string

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader

	mu       sync.Mutex
	pending  map[int64]chan *JSONRPCResponse
	closed   bool
	closeCh  chan struct{}
}

// NewStdioTransport creates a stdio transport
func NewStdioTransport(command string, args []string, env map[string]string) *StdioTransport {
	return &StdioTransport{
		command: command,
		args:    args,
		env:     env,
		pending: make(map[int64]chan *JSONRPCResponse),
		closeCh: make(chan struct{}),
	}
}

// Start spawns the subprocess and begins reading responses
func (t *StdioTransport) Start(ctx context.Context) error {
	resolvedCmd, err := exec.LookPath(t.command)
	if err != nil {
		return fmt.Errorf("mcp stdio: command not found: %s: %w", t.command, err)
	}

	t.cmd = exec.CommandContext(ctx, resolvedCmd, t.args...)

	// Build safe environment
	t.cmd.Env = buildSafeEnv(t.env)
	t.cmd.Stderr = os.Stderr // let server errors show

	var stdinPipe io.WriteCloser
	stdinPipe, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp stdio: stdin pipe: %w", err)
	}
	t.stdin = stdinPipe

	stdoutPipe, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp stdio: stdout pipe: %w", err)
	}
	t.stdout = bufio.NewReaderSize(stdoutPipe, 1024*1024) // 1MB buffer

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("mcp stdio: start %s: %w", t.command, err)
	}

	log.Info("mcp stdio: process started", "command", t.command, "pid", t.cmd.Process.Pid)

	// Background reader goroutine
	go t.readLoop()

	return nil
}

// Send sends a JSON-RPC request and waits for the response
func (t *StdioTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil, fmt.Errorf("mcp stdio: transport closed")
	}

	ch := make(chan *JSONRPCResponse, 1)
	t.pending[req.ID] = ch
	t.mu.Unlock()

	// Encode and send
	data, err := json.Marshal(req)
	if err != nil {
		t.removePending(req.ID)
		return nil, fmt.Errorf("mcp stdio: marshal request: %w", err)
	}

	// MCP stdio protocol: newline-delimited JSON
	data = append(data, '\n')
	if _, err := t.stdin.Write(data); err != nil {
		t.removePending(req.ID)
		return nil, fmt.Errorf("mcp stdio: write: %w", err)
	}

	// Wait for response or context cancellation
	select {
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("mcp stdio: connection closed while waiting for response")
		}
		return resp, nil
	case <-ctx.Done():
		t.removePending(req.ID)
		return nil, ctx.Err()
	case <-t.closeCh:
		return nil, fmt.Errorf("mcp stdio: transport closed while waiting")
	}
}

// Close shuts down the subprocess
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.closeCh)

	// Signal all pending requests
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
	t.mu.Unlock()

	// Close stdin to signal EOF to the subprocess
	if t.stdin != nil {
		_ = t.stdin.Close()
	}

	// Kill process if still running
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}

	log.Info("mcp stdio: process stopped", "command", t.command)
	return nil
}

func (t *StdioTransport) readLoop() {
	defer func() {
		t.mu.Lock()
		t.closed = true
		for id, ch := range t.pending {
			close(ch)
			delete(t.pending, id)
		}
		t.mu.Unlock()
	}()

	for {
		select {
		case <-t.closeCh:
			return
		default:
		}

		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Debug("mcp stdio: read error", "error", err)
			}
			return
		}

		line = bytes_TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var resp JSONRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			log.Debug("mcp stdio: invalid JSON response", "error", err, "line", string(line[:min(len(line), 200)]))
			continue
		}

		// Notifications (id=0 or missing) are logged and discarded for now
		if resp.ID == 0 {
			log.Debug("mcp stdio: notification received", "data", string(line[:min(len(line), 200)]))
			continue
		}

		t.mu.Lock()
		ch, ok := t.pending[resp.ID]
		if ok {
			delete(t.pending, resp.ID)
		}
		t.mu.Unlock()

		if ok {
			ch <- &resp
		}
	}
}

func (t *StdioTransport) removePending(id int64) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

// buildSafeEnv constructs a minimal, safe environment for the subprocess
func buildSafeEnv(userEnv map[string]string) []string {
	// Start with PATH and HOME from the current environment
	safeKeys := []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TERM", "TMPDIR"}
	env := make(map[string]string)
	for _, key := range safeKeys {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}

	// Overlay user-specified environment variables
	for k, v := range userEnv {
		env[k] = os.ExpandEnv(v) // resolve ${VAR} references
	}

	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// bytes_TrimSpace trims whitespace from byte slice
func bytes_TrimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\n' || b[start] == '\r') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\n' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sanitizeError strips credentials from error messages
func sanitizeError(text string) string {
	// Strip API keys and tokens from error messages
	patterns := []string{
		"sk-", "ghp_", "gho_", "Bearer ", "token=", "key=",
	}
	result := text
	for _, p := range patterns {
		if idx := strings.Index(result, p); idx >= 0 {
			// Find the end of the credential (space, quote, or end)
			end := idx + len(p)
			for end < len(result) && result[end] != ' ' && result[end] != '"' && result[end] != '\'' && result[end] != '\n' {
				end++
			}
			result = result[:idx] + "[REDACTED]" + result[end:]
		}
	}
	return result
}
