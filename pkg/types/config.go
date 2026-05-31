package types

// AgentConfig Agent 配置
type AgentConfig struct {
	// 模型配置
	Model       string  `json:"model" yaml:"model"` // e.g. "openai/gpt-4o"
	APIKey      string  `json:"api_key" yaml:"api_key"`
	BaseURL     string  `json:"base_url" yaml:"base_url"`
	Temperature float64 `json:"temperature" yaml:"temperature"`
	MaxTokens   int     `json:"max_tokens" yaml:"max_tokens"`

	// 自定义 Provider
	CustomProviders []CustomProviderConfig `json:"custom_providers" yaml:"custom_providers"`

	// Agent 行为
	MaxIterations   int      `json:"max_iterations" yaml:"max_iterations"` // 默认 90
	EnabledToolsets []string `json:"enabled_toolsets" yaml:"enabled_toolsets"`
	DisabledTools   []string `json:"disabled_tools" yaml:"disabled_tools"`
	Platform        string   `json:"platform" yaml:"platform"` // cli/telegram/discord...
	SessionID       string   `json:"session_id" yaml:"session_id"`
	WorkDir         string   `json:"work_dir" yaml:"work_dir"`

	// GraphPath points to a custom orchestrator Graph JSON file. When empty,
	// the built-in default graph is used. Overridable via HERMES_GRAPH.
	GraphPath string `json:"graph_path" yaml:"graph_path"`

	// 记忆
	SkipMemory bool `json:"skip_memory" yaml:"skip_memory"`

	// 并行
	MaxParallelTools int `json:"max_parallel_tools" yaml:"max_parallel_tools"` // 默认 8

	// 子 Agent
	DelegateMaxIterations int `json:"delegate_max_iterations" yaml:"delegate_max_iterations"` // 默认 50
	MaxDelegateDepth      int `json:"max_delegate_depth" yaml:"max_delegate_depth"`           // 默认 2
}

// CustomProviderConfig 自定义模型 Provider 配置
type CustomProviderConfig struct {
	Name    string                       `json:"name" yaml:"name"`         // provider 名称，如 "doubao"
	BaseURL string                       `json:"base_url" yaml:"base_url"` // API 地址
	APIKey  string                       `json:"api_key" yaml:"api_key"`   // API Key
	Model   string                       `json:"model" yaml:"model"`       // 默认模型名
	Models  map[string]CustomModelConfig `json:"models" yaml:"models"`     // 模型详细配置
}

// CustomModelConfig 单个自定义模型的配置
type CustomModelConfig struct {
	ContextLength int `json:"context_length" yaml:"context_length"`
}

// DefaultAgentConfig 返回默认配置
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		Model:                 "openai/gpt-4o",
		Temperature:           0.7,
		MaxTokens:             16384,
		MaxIterations:         90,
		MaxParallelTools:      8,
		DelegateMaxIterations: 50,
		MaxDelegateDepth:      2,
		Platform:              "cli",
	}
}
