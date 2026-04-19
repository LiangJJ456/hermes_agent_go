package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const writeFileToolName = "write_file"

func init() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    writeFileToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: writeFileToolName,
				Description: "Write content to a file, creating it if it doesn't exist or overwriting if it does. " +
					"Automatically creates parent directories as needed.\n\n" +
					"USE write_file WHEN:\n" +
					"- Creating a new file from scratch\n" +
					"- Completely replacing file contents\n" +
					"- Writing generated code, configs, or documents\n\n" +
					"USE edit_file INSTEAD WHEN:\n" +
					"- Making targeted changes to an existing file\n" +
					"- Replacing specific sections while preserving the rest",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Absolute or relative path for the file.",
						},
						"content": map[string]any{
							"type":        "string",
							"description": "The full content to write to the file.",
						},
					},
					"required": []string{"path", "content"},
				}),
			},
		},
		Handler:       handleWriteFile,
		NeverParallel: true,
	})
}

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func handleWriteFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Path == "" {
		return toolErr("path is required"), nil
	}

	dir := filepath.Dir(args.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return toolErr("Failed to create directory %s: %v", dir, err), nil
	}

	tmpPath := args.Path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(args.Content), 0o644); err != nil {
		return toolErr("Failed to write file: %v", err), nil
	}

	if err := os.Rename(tmpPath, args.Path); err != nil {
		_ = os.Remove(tmpPath)
		return toolErr("Failed to rename temp file: %v", err), nil
	}

	lines := strings.Count(args.Content, "\n")
	if !strings.HasSuffix(args.Content, "\n") && len(args.Content) > 0 {
		lines++
	}

	log.Debug("write_file: completed", "path", args.Path, "bytes", len(args.Content), "lines", lines)

	return fmt.Sprintf("Successfully wrote %d bytes (%d lines) to %s", len(args.Content), lines, args.Path), nil
}
