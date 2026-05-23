package model

import (
	"fmt"
	"strings"
	"sync"
)

// Router 模型路由器，根据 "provider/model" 格式分发到对应 Provider
type Router struct {
	mu        sync.RWMutex
	providers map[string]Provider // key = provider name (e.g. "openai")
}

// NewRouter 创建路由器
func NewRouter() *Router {
	return &Router{
		providers: make(map[string]Provider),
	}
}

// Register 注册供应商
func (r *Router) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// Resolve 根据 "provider/model" 解析出 Provider + model name
// 例: "openai/gpt-4o" → (openaiProvider, "gpt-4o")
func (r *Router) Resolve(modelSpec string) (Provider, string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	parts := strings.SplitN(modelSpec, "/", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid model spec %q, expected 'provider/model'", modelSpec)
	}

	providerName, modelName := parts[0], parts[1]
	p, ok := r.providers[providerName]
	if !ok {
		return nil, "", fmt.Errorf("unknown provider %q, registered: %v", providerName, r.providerNames())
	}
	return p, modelName, nil
}

// FirstRegistered 返回第一个注册的 Provider（用于自动选择默认）
func (r *Router) FirstRegistered() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.providers {
		return p
	}
	return nil
}

func (r *Router) providerNames() []string {
	names := make([]string, 0, len(r.providers))
	for k := range r.providers {
		names = append(names, k)
	}
	return names
}
