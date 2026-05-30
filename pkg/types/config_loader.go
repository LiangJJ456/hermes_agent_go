package types

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// LoadConfig loads config from files and environment variables.
// Priority: env > config file > defaults.
// Supports JSON (.json) and YAML (.yaml/.yml) formats.
func LoadConfig() AgentConfig {
	cfg := DefaultAgentConfig()

	// 1. Try loading from file (JSON first, then YAML)
	configPaths := []string{
		"hermes.json",
		"hermes.yaml",
		"hermes.yml",
		filepath.Join(hermesHomeDir(), "config.json"),
		filepath.Join(hermesHomeDir(), "config.yaml"),
		filepath.Join(hermesHomeDir(), "config.yml"),
	}

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var fileCfg AgentConfig
		ext := filepath.Ext(path)
		switch ext {
		case ".yaml", ".yml":
			normalized, err := normalizeLegacyYAML(data)
			if err == nil {
				data = normalized
			}
			if err := yaml.Unmarshal(data, &fileCfg); err != nil {
				continue
			}
		default:
			if err := json.Unmarshal(data, &fileCfg); err != nil {
				continue
			}
		}

		mergeConfig(&cfg, &fileCfg)
		break // only load the first found file
	}

	// 2. Override with environment variables
	if v := os.Getenv("HERMES_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.APIKey = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("HERMES_MAX_ITERATIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxIterations = n
		}
	}
	if v := os.Getenv("HERMES_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temperature = f
		}
	}
	if v := os.Getenv("HERMES_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxTokens = n
		}
	}
	if v := os.Getenv("HERMES_PLATFORM"); v != "" {
		cfg.Platform = v
	}
	if v := os.Getenv("HERMES_WORKDIR"); v != "" {
		cfg.WorkDir = v
	}
	if v := os.Getenv("HERMES_GRAPH"); v != "" {
		cfg.GraphPath = v
	}
	if os.Getenv("HERMES_SKIP_MEMORY") == "1" {
		cfg.SkipMemory = true
	}
	if v := os.Getenv("HERMES_MAX_PARALLEL"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxParallelTools = n
		}
	}

	return cfg
}

// normalizeLegacyYAML converts old-style nested model config to flat format.
//
// Old format:
//
//	model:
//	  default: ep-20260415120037-dtjmn
//	  provider: custom          # or e.g. "doubao"
//	  api_key: ...
//	  base_url: ...
//
// New format:
//
//	model: "doubao/ep-20260415120037-dtjmn"
//	api_key: ...
//	base_url: ...
func normalizeLegacyYAML(data []byte) ([]byte, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	modelVal, ok := raw["model"]
	if !ok {
		return data, nil
	}

	modelMap, isMap := modelVal.(map[string]any)
	if !isMap {
		return data, nil // already flat format
	}

	// Extract fields from nested model
	provider, _ := modelMap["provider"].(string)
	defaultModel, _ := modelMap["default"].(string)

	// Promote api_key / base_url from nested model to top-level if not already set
	if _, exists := raw["api_key"]; !exists {
		if v, ok := modelMap["api_key"].(string); ok {
			raw["api_key"] = v
		}
	}
	if _, exists := raw["base_url"]; !exists {
		if v, ok := modelMap["base_url"].(string); ok {
			raw["base_url"] = v
		}
	}

	// Build flat model string
	if defaultModel != "" {
		if provider != "" && provider != "custom" {
			raw["model"] = provider + "/" + defaultModel
		} else {
			// "custom" provider: try to match from custom_providers by api_key/base_url
			raw["model"] = resolveCustomModel(raw, defaultModel)
		}
	} else {
		delete(raw, "model")
	}

	return yaml.Marshal(raw)
}

// resolveCustomModel matches "custom" provider to a custom_providers entry
// and builds the flat model string.
func resolveCustomModel(raw map[string]any, defaultModel string) string {
	providers, _ := raw["custom_providers"].([]any)
	apiKey, _ := raw["api_key"].(string)
	baseURL, _ := raw["base_url"].(string)

	for _, p := range providers {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, _ := pm["name"].(string)
		if name == "" {
			continue
		}
		// Match by api_key or base_url, or just take the first one
		if len(providers) == 1 ||
			(apiKey != "" && pm["api_key"] == apiKey) ||
			(baseURL != "" && pm["base_url"] == baseURL) {
			return name + "/" + defaultModel
		}
	}

	// Fallback: just use the model name as-is
	return defaultModel
}

// mergeConfig merges non-zero values from src into dst.
func mergeConfig(dst, src *AgentConfig) {
	if src.Model != "" {
		dst.Model = src.Model
	}
	if src.APIKey != "" {
		dst.APIKey = src.APIKey
	}
	if src.BaseURL != "" {
		dst.BaseURL = src.BaseURL
	}
	if src.Temperature > 0 {
		dst.Temperature = src.Temperature
	}
	if src.MaxTokens > 0 {
		dst.MaxTokens = src.MaxTokens
	}
	if src.MaxIterations > 0 {
		dst.MaxIterations = src.MaxIterations
	}
	if len(src.EnabledToolsets) > 0 {
		dst.EnabledToolsets = src.EnabledToolsets
	}
	if len(src.DisabledTools) > 0 {
		dst.DisabledTools = src.DisabledTools
	}
	if len(src.CustomProviders) > 0 {
		dst.CustomProviders = src.CustomProviders
	}
	if src.Platform != "" {
		dst.Platform = src.Platform
	}
	if src.WorkDir != "" {
		dst.WorkDir = src.WorkDir
	}
	if src.SkipMemory {
		dst.SkipMemory = true
	}
	if src.MaxParallelTools > 0 {
		dst.MaxParallelTools = src.MaxParallelTools
	}
	if src.DelegateMaxIterations > 0 {
		dst.DelegateMaxIterations = src.DelegateMaxIterations
	}
	if src.MaxDelegateDepth > 0 {
		dst.MaxDelegateDepth = src.MaxDelegateDepth
	}
}

func hermesHomeDir() string {
	if v := os.Getenv("HERMES_HOME"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes")
}
