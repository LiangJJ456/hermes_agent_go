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

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/model"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const defaultBaseURL = "https://api.openai.com/v1"

// Provider OpenAI 兼容 Provider
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// New 创建 OpenAI Provider
func New(apiKey, baseURL string) *Provider {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

func (p *Provider) Name() string { return "openai" }

func (p *Provider) SupportsTools() bool { return true }

func (p *Provider) MaxContextTokens() int { return 128000 }

// Chat 同步调用
func (p *Provider) Chat(ctx context.Context, req *model.ChatRequest) (*types.ChatResponse, error) {
	body := p.buildRequestBody(req, false)

	respBody, err := p.doRequest(ctx, body)
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

// ChatStream 流式调用
func (p *Provider) ChatStream(ctx context.Context, req *model.ChatRequest, cb model.StreamCallback) (*types.ChatResponse, error) {
	body := p.buildRequestBody(req, true)

	respBody, err := p.doRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	defer respBody.Close()

	// SSE 解析
	var fullMsg types.Message
	fullMsg.Role = types.RoleAssistant
	var usage types.Usage
	toolCallsMap := make(map[int]*types.ToolCall)

	decoder := json.NewDecoder(respBody)
	buf := make([]byte, 0, 4096)
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

		// 累积 tool_calls
		for _, tc := range delta.ToolCalls {
			existing, ok := toolCallsMap[tc.Index]
			if !ok {
				existing = &types.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
				}
				existing.Function.Name = tc.Function.Name
				toolCallsMap[tc.Index] = existing
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

	_ = buf
	_ = decoder
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

func (p *Provider) doRequest(ctx context.Context, body map[string]any) (io.ReadCloser, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Warn("API error", "status", resp.StatusCode, "body", string(body))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return resp.Body, nil
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
