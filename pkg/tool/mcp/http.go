package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// HTTPTransport communicates with an MCP server via HTTP (Streamable HTTP).
// Each JSON-RPC request is a POST to the server URL; responses come back
// in the HTTP response body.
type HTTPTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
}

// NewHTTPTransport creates an HTTP transport
func NewHTTPTransport(url string, headers map[string]string, connectTimeout time.Duration) *HTTPTransport {
	return &HTTPTransport{
		url:     url,
		headers: headers,
		client: &http.Client{
			Timeout: connectTimeout,
		},
	}
}

// Start validates the URL (no subprocess to spawn)
func (t *HTTPTransport) Start(_ context.Context) error {
	if t.url == "" {
		return fmt.Errorf("mcp http: url is required")
	}
	log.Info("mcp http: transport ready", "url", t.url)
	return nil
}

// Send sends a JSON-RPC request via HTTP POST
func (t *HTTPTransport) Send(ctx context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("mcp http: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("mcp http: create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	httpResp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp http: request failed: %w", sanitizeHTTPError(err))
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 2048))
		return nil, fmt.Errorf("mcp http: server returned %d: %s", httpResp.StatusCode, sanitizeError(string(body)))
	}

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("mcp http: read response: %w", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("mcp http: invalid JSON response: %w", err)
	}

	return &resp, nil
}

// Close is a no-op for HTTP transport
func (t *HTTPTransport) Close() error {
	t.client.CloseIdleConnections()
	return nil
}

func sanitizeHTTPError(err error) error {
	return fmt.Errorf("%s", sanitizeError(err.Error()))
}
