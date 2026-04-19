package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
)

// LoadConfig reads MCP server configuration from the Hermes config file.
//
// Config file: $HERMES_HOME/mcp_servers.json or ~/.hermes/mcp_servers.json
//
// Example mcp_servers.json:
//
//	{
//	  "filesystem": {
//	    "command": "npx",
//	    "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
//	  },
//	  "remote": {
//	    "url": "https://my-mcp-server.example.com/mcp",
//	    "headers": {"Authorization": "Bearer ${MCP_TOKEN}"}
//	  }
//	}
//
// Also supports config.json with "mcp_servers" key:
//
//	{"mcp_servers": { ... }}
func LoadConfig() (map[string]ServerConfig, error) {
	// Try dedicated mcp_servers.json first
	configs, err := loadDedicatedConfig()
	if err == nil && configs != nil {
		return expandEnvAll(configs), nil
	}

	// Fall back to config.json with mcp_servers key
	configs, err = loadFromMainConfig()
	if err != nil {
		return nil, err
	}
	if configs == nil {
		return nil, nil
	}
	return expandEnvAll(configs), nil
}

func loadDedicatedConfig() (map[string]ServerConfig, error) {
	path := findFile("mcp_servers.json")
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var configs map[string]ServerConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	log.Info("mcp: loaded dedicated config", "servers", len(configs), "file", path)
	return configs, nil
}

func loadFromMainConfig() (map[string]ServerConfig, error) {
	path := findFile("config.json")
	if path == "" {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw struct {
		MCPServers map[string]ServerConfig `json:"mcp_servers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if len(raw.MCPServers) == 0 {
		return nil, nil
	}

	log.Info("mcp: loaded config from main config", "servers", len(raw.MCPServers), "file", path)
	return raw.MCPServers, nil
}

func findFile(name string) string {
	if home := os.Getenv("HERMES_HOME"); home != "" {
		p := filepath.Join(home, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".hermes", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func expandEnvAll(configs map[string]ServerConfig) map[string]ServerConfig {
	for name, cfg := range configs {
		cfg.Command = os.ExpandEnv(cfg.Command)
		cfg.URL = os.ExpandEnv(cfg.URL)

		expanded := make([]string, len(cfg.Args))
		for i, a := range cfg.Args {
			expanded[i] = os.ExpandEnv(a)
		}
		cfg.Args = expanded

		cfg.Env = expandMap(cfg.Env)
		cfg.Headers = expandMap(cfg.Headers)

		configs[name] = cfg
	}
	return configs
}

func expandMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = os.ExpandEnv(v)
	}
	return out
}
