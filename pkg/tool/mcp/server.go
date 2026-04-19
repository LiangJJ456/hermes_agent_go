package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// Server represents a single MCP server connection.
// It manages the connection lifecycle, tool discovery, and tool call dispatch.
type Server struct {
	Name      string
	Config    ServerConfig
	State     ServerState
	Error     string
	Tools     []MCPToolDef
	ToolNames []string // prefixed names registered in the tool registry

	transport Transport
	mu        sync.RWMutex
}

// NewServer creates a server instance (not yet connected)
func NewServer(name string, config ServerConfig) *Server {
	return &Server{
		Name:   name,
		Config: config,
		State:  StateDisconnected,
	}
}

// Connect establishes the connection and discovers tools
func (s *Server) Connect(ctx context.Context) error {
	s.mu.Lock()
	s.State = StateConnecting
	s.mu.Unlock()

	// Create transport based on config
	switch s.Config.Transport() {
	case TransportStdio:
		if s.Config.Command == "" {
			return fmt.Errorf("mcp server '%s': command is required for stdio transport", s.Name)
		}
		s.transport = NewStdioTransport(s.Config.Command, s.Config.Args, s.Config.Env)
	case TransportHTTP:
		s.transport = NewHTTPTransport(s.Config.URL, s.Config.Headers, s.Config.GetConnectTimeout())
	}

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, s.Config.GetConnectTimeout())
	defer cancel()

	if err := s.transport.Start(connectCtx); err != nil {
		s.mu.Lock()
		s.State = StateError
		s.Error = sanitizeError(err.Error())
		s.mu.Unlock()
		return fmt.Errorf("mcp server '%s': connect failed: %w", s.Name, err)
	}

	// Initialize MCP session
	if err := s.initialize(connectCtx); err != nil {
		_ = s.transport.Close()
		s.mu.Lock()
		s.State = StateError
		s.Error = sanitizeError(err.Error())
		s.mu.Unlock()
		return fmt.Errorf("mcp server '%s': initialize failed: %w", s.Name, err)
	}

	// Discover tools
	if err := s.discoverTools(connectCtx); err != nil {
		_ = s.transport.Close()
		s.mu.Lock()
		s.State = StateError
		s.Error = sanitizeError(err.Error())
		s.mu.Unlock()
		return fmt.Errorf("mcp server '%s': tool discovery failed: %w", s.Name, err)
	}

	s.mu.Lock()
	s.State = StateReady
	s.Error = ""
	s.mu.Unlock()

	log.Info("mcp server ready", "name", s.Name, "tools", len(s.Tools))
	return nil
}

// initialize sends the MCP initialize request
func (s *Server) initialize(ctx context.Context) error {
	params, _ := json.Marshal(map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"roots": map[string]any{"listChanged": true},
		},
		"clientInfo": map[string]any{
			"name":    "hermes-agent-go",
			"version": "1.0.0",
		},
	})

	resp, err := s.transport.Send(ctx, &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      nextRequestID(),
		Method:  "initialize",
		Params:  params,
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}

	// Send initialized notification (no response expected, but we still use Send for stdio)
	// For notifications, we don't wait for a response — fire and forget via a goroutine
	go func() {
		notifyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = s.transport.Send(notifyCtx, &JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      nextRequestID(),
			Method:  "notifications/initialized",
		})
	}()

	return nil
}

// discoverTools fetches the tool list from the server
func (s *Server) discoverTools(ctx context.Context) error {
	resp, err := s.transport.Send(ctx, &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      nextRequestID(),
		Method:  "tools/list",
	})
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("tools/list error: [%d] %s", resp.Error.Code, resp.Error.Message)
	}

	var result struct {
		Tools []MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return fmt.Errorf("tools/list: invalid result: %w", err)
	}

	// Apply include/exclude filter
	s.Tools = filterTools(result.Tools, s.Config.Tools)

	log.Debug("mcp: tools discovered",
		"server", s.Name,
		"total", len(result.Tools),
		"filtered", len(s.Tools))

	return nil
}

// CallTool calls a tool on the server
func (s *Server) CallTool(ctx context.Context, toolName string, args map[string]any) (string, error) {
	s.mu.RLock()
	if s.State != StateReady {
		s.mu.RUnlock()
		return "", fmt.Errorf("mcp server '%s' is not ready (state: %d)", s.Name, s.State)
	}
	s.mu.RUnlock()

	// Build params
	params, _ := json.Marshal(MCPToolCallParams{
		Name:      toolName,
		Arguments: args,
	})

	// Call with timeout
	callCtx, cancel := context.WithTimeout(ctx, s.Config.GetTimeout())
	defer cancel()

	resp, err := s.transport.Send(callCtx, &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      nextRequestID(),
		Method:  "tools/call",
		Params:  params,
	})
	if err != nil {
		return "", fmt.Errorf("mcp tool call '%s': %w", toolName, err)
	}
	if resp.Error != nil {
		return "", fmt.Errorf("mcp tool '%s' error: [%d] %s",
			toolName, resp.Error.Code, sanitizeError(resp.Error.Message))
	}

	// Parse result
	var result MCPToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("mcp tool '%s': invalid result: %w", toolName, err)
	}

	// Concatenate text content
	var sb strings.Builder
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(c.Text)
		case "image":
			sb.WriteString("[image: ")
			sb.WriteString(c.MimeType)
			sb.WriteString("]")
		case "resource":
			sb.WriteString("[resource]")
		}
	}

	output := sb.String()
	if result.IsError {
		return sanitizeError(output), fmt.Errorf("mcp tool '%s' returned error", toolName)
	}

	return output, nil
}

// Shutdown gracefully closes the server connection
func (s *Server) Shutdown() {
	s.mu.Lock()
	s.State = StateShuttingDown
	s.mu.Unlock()

	if s.transport != nil {
		_ = s.transport.Close()
	}

	s.mu.Lock()
	s.State = StateDisconnected
	s.mu.Unlock()

	log.Info("mcp server stopped", "name", s.Name)
}

// Status returns the server's current status
func (s *Server) Status() ServerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	transport := "stdio"
	if s.Config.Transport() == TransportHTTP {
		transport = "http"
	}

	toolNames := make([]string, len(s.Tools))
	for i, t := range s.Tools {
		toolNames[i] = t.Name
	}

	return ServerStatus{
		Name:      s.Name,
		State:     s.State,
		Transport: transport,
		Tools:     toolNames,
		Error:     s.Error,
	}
}

// filterTools applies include/exclude filtering
func filterTools(tools []MCPToolDef, filter *ToolFilter) []MCPToolDef {
	if filter == nil {
		return tools
	}

	includeSet := toSet(filter.Include)
	excludeSet := toSet(filter.Exclude)

	var filtered []MCPToolDef
	for _, t := range tools {
		if len(includeSet) > 0 {
			if _, ok := includeSet[t.Name]; !ok {
				continue
			}
		} else if len(excludeSet) > 0 {
			if _, ok := excludeSet[t.Name]; ok {
				continue
			}
		}
		filtered = append(filtered, t)
	}
	return filtered
}

func toSet(items []string) map[string]struct{} {
	if len(items) == 0 {
		return nil
	}
	m := make(map[string]struct{}, len(items))
	for _, item := range items {
		m[item] = struct{}{}
	}
	return m
}
