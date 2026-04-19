// Package mcp implements the Model Context Protocol (MCP) client.
//
// It connects to external MCP servers via stdio or HTTP/SSE transport,
// discovers their tools, and registers them into the hermes-agent tool
// registry so the agent can call them like any built-in tool.
//
// Configuration is read from ~/.hermes/config.yaml under "mcp_servers".
package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// ───────────────────── MCP Protocol Types ─────────────────────

// JSONRPCRequest JSON-RPC 2.0 request
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse JSON-RPC 2.0 response
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError JSON-RPC 2.0 error
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPToolDef tool definition returned by tools/list
type MCPToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// MCPToolCallParams parameters for tools/call
type MCPToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// MCPToolResult result from tools/call
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError,omitempty"`
}

// MCPContent content block in tool results
type MCPContent struct {
	Type     string `json:"type"` // "text", "image", "resource"
	Text     string `json:"text,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"` // base64 for image
}

// ───────────────────── Server Configuration ─────────────────────

// ServerConfig MCP server configuration from config.yaml
type ServerConfig struct {
	// Stdio transport
	Command string            `yaml:"command" json:"command,omitempty"`
	Args    []string          `yaml:"args" json:"args,omitempty"`
	Env     map[string]string `yaml:"env" json:"env,omitempty"`

	// HTTP transport
	URL     string            `yaml:"url" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers" json:"headers,omitempty"`

	// Common
	Timeout        int `yaml:"timeout" json:"timeout,omitempty"`                 // per-tool-call timeout (seconds)
	ConnectTimeout int `yaml:"connect_timeout" json:"connect_timeout,omitempty"` // initial connection timeout (seconds)

	// Tool filtering
	Tools *ToolFilter `yaml:"tools" json:"tools,omitempty"`
}

// ToolFilter include/exclude filtering for tool registration
type ToolFilter struct {
	Include []string `yaml:"include" json:"include,omitempty"`
	Exclude []string `yaml:"exclude" json:"exclude,omitempty"`
}

// TransportType 传输类型
type TransportType int

const (
	TransportStdio TransportType = iota
	TransportHTTP
)

// Transport returns the transport type based on config
func (c *ServerConfig) Transport() TransportType {
	if c.URL != "" {
		return TransportHTTP
	}
	return TransportStdio
}

// GetTimeout returns tool call timeout with default
func (c *ServerConfig) GetTimeout() time.Duration {
	if c.Timeout > 0 {
		return time.Duration(c.Timeout) * time.Second
	}
	return 120 * time.Second
}

// GetConnectTimeout returns connection timeout with default
func (c *ServerConfig) GetConnectTimeout() time.Duration {
	if c.ConnectTimeout > 0 {
		return time.Duration(c.ConnectTimeout) * time.Second
	}
	return 60 * time.Second
}

// ───────────────────── Server State ─────────────────────

// ServerState MCP server connection state
type ServerState int

const (
	StateDisconnected ServerState = iota
	StateConnecting
	StateReady
	StateError
	StateShuttingDown
)

// ServerStatus runtime status of an MCP server
type ServerStatus struct {
	Name      string      `json:"name"`
	State     ServerState `json:"state"`
	Transport string      `json:"transport"` // "stdio" or "http"
	Tools     []string    `json:"tools"`
	Error     string      `json:"error,omitempty"`
}

// ───────────────────── Atomic ID generator ─────────────────────

var (
	reqIDMu sync.Mutex
	reqIDSeq int64
)

func nextRequestID() int64 {
	reqIDMu.Lock()
	defer reqIDMu.Unlock()
	reqIDSeq++
	return reqIDSeq
}

// ───────────────────── Transport interface ─────────────────────

// Transport abstracts stdio and HTTP/SSE communication
type Transport interface {
	// Start establishes the connection
	Start(ctx context.Context) error

	// Send sends a JSON-RPC request and waits for response
	Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error)

	// Close tears down the connection
	Close() error
}
