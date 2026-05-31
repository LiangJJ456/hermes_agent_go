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
