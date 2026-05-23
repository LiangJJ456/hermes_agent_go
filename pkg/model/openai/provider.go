package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent/credential"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Provider OpenAI 兼容 Provider
type Provider struct {
	name            string
	apiKey          string
	baseURL         string
	client          *http.Client
	retry           RetryConfig
	maxContextTokens int

	// 可选：凭据池（设置后优先使用池分配的凭据）
	credPool *credential.Pool
}

// New 创建 OpenAI Provider
func New(apiKey, baseURL string) *Provider {
	return NewWithName("openai", apiKey, baseURL, 128000)
}

// NewWithName 创建自定义名称的 OpenAI 兼容 Provider
func NewWithName(name, apiKey, baseURL string, maxContextTokens int) *Provider {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if maxContextTokens <= 0 {
		maxContextTokens = 128000
	}
	return &Provider{
		name:             name,
		apiKey:           apiKey,
		baseURL:          strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
		retry:            DefaultRetryConfig(),
		maxContextTokens: maxContextTokens,
	}
}

// SetCredentialPool 设置凭据池（启用多 key 轮转）
func (p *Provider) SetCredentialPool(pool *credential.Pool) {
	p.credPool = pool
}

// SetRetryConfig 设置自定义重试配置
func (p *Provider) SetRetryConfig(cfg RetryConfig) {
	p.retry = cfg
}

func (p *Provider) Name() string { return p.name }

func (p *Provider) SupportsTools() bool { return true }

func (p *Provider) MaxContextTokens() int { return p.maxContextTokens }

// Chat 同步调用（带重试）
func (p *Provider) Chat(ctx context.Context, req *model.ChatRequest) (*types.ChatResponse, error) {
	body := p.buildRequestBody(req, false)

	respBody, _, err := p.doRequestWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	var apiResp chatCompletionResponse
	if err := json.NewDecoder(respBody).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	choice := apiResp.Choices[0]
	msg := types.Message{
		Role:         types.RoleAssistant,
		Content:      choice.Message.Content,
		FinishReason: types.FinishReason(choice.FinishReason),
	}

	// 解析 tool_calls
	if len(choice.Message.ToolCalls) > 0 {
		for _, tc := range choice.Message.ToolCalls {
			msg.ToolCalls = append(msg.ToolCalls, types.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	usage := types.Usage{
		PromptTokens:     apiResp.Usage.PromptTokens,
		CompletionTokens: apiResp.Usage.CompletionTokens,
		TotalTokens:      apiResp.Usage.TotalTokens,
	}

	return &types.ChatResponse{Message: msg, Usage: usage}, nil
}

// ChatStream 流式调用（带重试）
func (p *Provider) ChatStream(ctx context.Context, req *model.ChatRequest, cb model.StreamCallback) (*types.ChatResponse, error) {
	body := p.buildRequestBody(req, true)

	respBody, _, err := p.doRequestWithRetry(ctx, body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	// SSE 解析
	var fullMsg types.Message
	fullMsg.Role = types.RoleAssistant
	var usage types.Usage
	toolCallsMap := make(map[int]*types.ToolCall)

	scanner := newSSEScanner(respBody)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk streamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			fullMsg.Content += delta.Content
			cb(types.StreamDelta{Content: delta.Content})
		}

		// 累积 tool_calls + 实时通知
		for _, tc := range delta.ToolCalls {
			existing, ok := toolCallsMap[tc.Index]
			if !ok {
				existing = &types.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				}
				existing.Function.Name = tc.Function.Name
				toolCallsMap[tc.Index] = existing
				// P1-17: 通知用户开始接收 tool_call
				if tc.Function.Name != "" {
					cb(types.StreamDelta{
						ToolCallStart: true,
						ToolCallName:  tc.Function.Name,
						ToolCallID:    tc.ID,
					})
				}
			} else {
				existing.Function.Arguments += tc.Function.Arguments
			}
		}

		if chunk.Choices[0].FinishReason != "" {
			fullMsg.FinishReason = types.FinishReason(chunk.Choices[0].FinishReason)
		}

		if chunk.Usage != nil {
			usage = types.Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}
	}

	// 组装 tool_calls
	for i := 0; i < len(toolCallsMap); i++ {
		if tc, ok := toolCallsMap[i]; ok {
			fullMsg.ToolCalls = append(fullMsg.ToolCalls, *tc)
		}
	}

	return &types.ChatResponse{Message: fullMsg, Usage: usage}, nil
}

// ── 内部方法 ──

func (p *Provider) buildRequestBody(req *model.ChatRequest, stream bool) map[string]any {
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, m := range req.Messages {
		msg := map[string]any{
			"role":    string(m.Role),
			"content": m.Content,
		}
		if m.Name != "" {
			msg["name"] = m.Name
		}
		if m.ToolCallID != "" {
			msg["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			msg["tool_calls"] = m.ToolCalls
		}
		messages = append(messages, msg)
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
	}

	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if stream {
		body["stream"] = true
		body["stream_options"] = map[string]any{"include_usage": true}
	}

	return body
}

// resolveCredential 获取当前应使用的凭据
func (p *Provider) resolveCredential() (apiKey, baseURL string, credID string) {
	// 优先从凭据池获取
	if p.credPool != nil {
		cred := p.credPool.Acquire()
		if cred != nil {
			return cred.APIKey, cred.BaseURL, cred.ID
		}
		// 池中无可用凭据，fallback 到默认
		log.Warn("credential pool exhausted, using default key")
	}
	return p.apiKey, p.baseURL, ""
}

// doRequestWithRetry 带重试和凭据池感知的请求
func (p *Provider) doRequestWithRetry(ctx context.Context, body map[string]any) (io.ReadCloser, int, error) {
	var lastErr error

	for attempt := 0; attempt <= p.retry.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := p.retry.Sleep(ctx, attempt-1); err != nil {
				return nil, 0, err // context cancelled
			}
		}

		apiKey, baseURL, credID := p.resolveCredential()
		if baseURL == "" {
			baseURL = p.baseURL
		}

		respBody, statusCode, err := p.doSingleRequest(ctx, body, apiKey, baseURL)
		if err == nil {
			return respBody, statusCode, nil
		}

		lastErr = err

		// 通知凭据池错误
		if credID != "" && p.credPool != nil && statusCode > 0 {
			p.credPool.HandleError(credID, statusCode)
		}

		// 判断是否可重试
		if statusCode > 0 && p.retry.IsRetryable(statusCode) {
			log.Warn("API request failed, retrying",
				"attempt", attempt+1,
				"max_retries", p.retry.MaxRetries,
				"status", statusCode,
				"error", err,
			)
			continue
		}

		// 不可重试的错误，直接返回
		return nil, statusCode, err
	}

	return nil, 0, fmt.Errorf("max retries (%d) exceeded: %w", p.retry.MaxRetries, lastErr)
}

// doSingleRequest 执行单次 HTTP 请求
func (p *Provider) doSingleRequest(ctx context.Context, body map[string]any, apiKey, baseURL string) (io.ReadCloser, int, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, 0, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Warn("API error", "status", resp.StatusCode, "body", string(respBytes))
		return nil, resp.StatusCode, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBytes))
	}

	return resp.Body, resp.StatusCode, nil
}

// ── API 响应结构 ──

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ── SSE Scanner ──

type sseScanner struct {
	reader *bufioReader
	text   string
}

type bufioReader struct {
	reader io.Reader
	buf    []byte
	start  int
	end    int
}

func newSSEScanner(r io.Reader) *sseScanner {
	return &sseScanner{
		reader: &bufioReader{reader: r, buf: make([]byte, 8192)},
	}
}

func (s *sseScanner) Scan() bool {
	var line []byte
	for {
		b, err := s.reader.ReadByte()
		if err != nil {
			if len(line) > 0 {
				s.text = string(line)
				return true
			}
			return false
		}
		if b == '\n' {
			s.text = string(line)
			return true
		}
		line = append(line, b)
	}
}

func (s *sseScanner) Text() string {
	return strings.TrimRight(s.text, "\r")
}

func (r *bufioReader) ReadByte() (byte, error) {
	if r.start >= r.end {
		n, err := r.reader.Read(r.buf)
		if n == 0 {
			return 0, err
		}
		r.start = 0
		r.end = n
	}
	b := r.buf[r.start]
	r.start++
	return b, nil
}
