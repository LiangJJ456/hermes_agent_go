package types

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadConfig_GraphPathFromFile guards that graph_path set in a config file
// is merged into the loaded config (regression: mergeConfig once dropped it).
func TestLoadConfig_GraphPathFromFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"graph_path":"/tmp/custom-graph.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Point HERMES_HOME at the temp config and ensure the env override is unset
	// so we are exercising the config-file path, not the env-var path.
	t.Setenv("HERMES_HOME", dir)
	t.Setenv("HERMES_GRAPH", "")

	cfg := LoadConfig()
	if cfg.GraphPath != "/tmp/custom-graph.json" {
		t.Fatalf("expected GraphPath from config file, got %q", cfg.GraphPath)
	}
}

func TestLoadConfig_ToolBackgroundAfterDefault(t *testing.T) {
	os.Unsetenv("HERMES_TOOL_BG_AFTER")
	cfg := LoadConfig()
	if cfg.ToolBackgroundAfter != 30 {
		t.Fatalf("default ToolBackgroundAfter should be 30, got %d", cfg.ToolBackgroundAfter)
	}
}

func TestLoadConfig_ToolBackgroundAfterEnv(t *testing.T) {
	os.Setenv("HERMES_TOOL_BG_AFTER", "5")
	defer os.Unsetenv("HERMES_TOOL_BG_AFTER")
	cfg := LoadConfig()
	if cfg.ToolBackgroundAfter != 5 {
		t.Fatalf("env should override to 5, got %d", cfg.ToolBackgroundAfter)
	}
}
