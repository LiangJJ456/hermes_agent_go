package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	// CircuitBreakerThreshold — consecutive failures before short-circuiting
	CircuitBreakerThreshold = 5
)

// Manager manages all MCP server connections and tool registration.
type Manager struct {
	mu      sync.RWMutex
	servers map[string]*Server

	// Circuit breaker: consecutive error counts per server
	errorCounts map[string]int
}

// NewManager creates an MCP manager
func NewManager() *Manager {
	return &Manager{
		servers:     make(map[string]*Server),
		errorCounts: make(map[string]int),
	}
}

// DiscoverAndRegister connects to all configured MCP servers,
// discovers their tools, and registers them in the global tool registry.
func (m *Manager) DiscoverAndRegister(ctx context.Context, configs map[string]ServerConfig) error {
	if len(configs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string

	for name, cfg := range configs {
		wg.Add(1)
		go func(name string, cfg ServerConfig) {
			defer wg.Done()

			server := NewServer(name, cfg)
			if err := server.Connect(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Sprintf("%s: %s", name, sanitizeError(err.Error())))
				mu.Unlock()
				log.Warn("mcp: server connection failed", "name", name, "error", err)
				return
			}

			m.mu.Lock()
			m.servers[name] = server
			m.mu.Unlock()

			// Register tools
			registered := m.registerServerTools(name, server, cfg)
			log.Info("mcp: server tools registered",
				"server", name,
				"count", len(registered))
		}(name, cfg)
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("mcp: %d server(s) failed: %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}

// registerServerTools registers a server's tools into the global registry
func (m *Manager) registerServerTools(serverName string, server *Server, config ServerConfig) []string {
	reg := registry.Global()
	toolsetName := "mcp-" + serverName
	var registered []string

	for _, mcpTool := range server.Tools {
		// Prefix tool name with server name to avoid collisions
		prefixedName := sanitizeName(serverName) + "__" + sanitizeName(mcpTool.Name)

		// Check for collision with built-in tools
		if existing, ok := reg.Get(prefixedName); ok {
			log.Warn("mcp: tool name collision, skipping",
				"server", serverName,
				"tool", prefixedName,
				"existing_toolset", existing.Toolset)
			continue
		}

		// Build schema
		schema := convertMCPSchema(serverName, mcpTool, prefixedName)

		// Capture for closure
		toolName := mcpTool.Name
		srvName := serverName

		handler := func(ctx context.Context, raw json.RawMessage) (string, error) {
			// Circuit breaker check
			m.mu.RLock()
			count := m.errorCounts[srvName]
			m.mu.RUnlock()

			if count >= CircuitBreakerThreshold {
				return fmt.Sprintf(`{"error":"MCP server '%s' is unavailable after %d consecutive failures. Use alternative tools."}`,
					srvName, count), nil
			}

			// Parse args
			var args map[string]any
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &args); err != nil {
					return fmt.Sprintf(`{"error":"invalid arguments: %s"}`, err.Error()), nil
				}
			}

			// Call
			m.mu.RLock()
			srv, ok := m.servers[srvName]
			m.mu.RUnlock()
			if !ok {
				return fmt.Sprintf(`{"error":"MCP server '%s' not found"}`, srvName), nil
			}

			result, err := srv.CallTool(ctx, toolName, args)
			if err != nil {
				m.mu.Lock()
				m.errorCounts[srvName]++
				m.mu.Unlock()
				return fmt.Sprintf(`{"error":"%s"}`, sanitizeError(err.Error())), nil
			}

			// Reset error count on success
			m.mu.Lock()
			m.errorCounts[srvName] = 0
			m.mu.Unlock()

			return result, nil
		}

		err := reg.Register(&registry.ToolEntry{
			Name:         prefixedName,
			Toolset:      toolsetName,
			Schema:       schema,
			Handler:      handler,
			ParallelSafe: true, // MCP tools are generally stateless
		})
		if err != nil {
			log.Warn("mcp: tool registration failed",
				"server", serverName,
				"tool", prefixedName,
				"error", err)
			continue
		}

		registered = append(registered, prefixedName)
	}

	// Store registered names on the server
	server.mu.Lock()
	server.ToolNames = registered
	server.mu.Unlock()

	return registered
}

// GetStatus returns status for all servers
func (m *Manager) GetStatus() []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]ServerStatus, 0, len(m.servers))
	for _, s := range m.servers {
		statuses = append(statuses, s.Status())
	}
	return statuses
}

// GetServer returns a specific server
func (m *Manager) GetServer(name string) (*Server, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.servers[name]
	return s, ok
}

// ShutdownAll gracefully shuts down all servers
func (m *Manager) ShutdownAll() {
	m.mu.Lock()
	servers := make([]*Server, 0, len(m.servers))
	for _, s := range m.servers {
		servers = append(servers, s)
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range servers {
		wg.Add(1)
		go func(srv *Server) {
			defer wg.Done()
			srv.Shutdown()
		}(s)
	}
	wg.Wait()

	m.mu.Lock()
	m.servers = make(map[string]*Server)
	m.errorCounts = make(map[string]int)
	m.mu.Unlock()

	log.Info("mcp: all servers shut down")
}

// ───────────────────── Schema Conversion ─────────────────────

// convertMCPSchema converts MCP tool definition to OpenAI-compatible tool schema
func convertMCPSchema(serverName string, tool MCPToolDef, prefixedName string) types.ToolSchema {
	// Normalize input schema
	var params json.RawMessage
	if len(tool.InputSchema) > 0 {
		params = normalizeInputSchema(tool.InputSchema)
	} else {
		params, _ = json.Marshal(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		})
	}

	description := tool.Description
	if description == "" {
		description = fmt.Sprintf("MCP tool '%s' from server '%s'", tool.Name, serverName)
	}

	// Prepend server name for context
	description = fmt.Sprintf("[MCP: %s] %s", serverName, description)

	return types.ToolSchema{
		Type: "function",
		Function: types.FunctionSchema{
			Name:        prefixedName,
			Description: description,
			Parameters:  params,
		},
	}
}

// normalizeInputSchema ensures the schema has "type": "object"
func normalizeInputSchema(schema json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(schema, &m); err != nil {
		// Fall back to empty object schema
		b, _ := json.Marshal(map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		})
		return b
	}

	// Ensure type is "object"
	if _, ok := m["type"]; !ok {
		m["type"] = "object"
	}
	if _, ok := m["properties"]; !ok {
		m["properties"] = map[string]any{}
	}

	// Remove unsupported fields for OpenAI
	delete(m, "$schema")
	delete(m, "additionalProperties")

	b, _ := json.Marshal(m)
	return b
}

// sanitizeName makes a name safe for use as a tool name component
func sanitizeName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '-', r == '_':
			sb.WriteRune('_')
		default:
			sb.WriteRune('_')
		}
	}
	result := sb.String()
	// Trim leading/trailing underscores
	result = strings.Trim(result, "_")
	if result == "" {
		return "unnamed"
	}
	return result
}
