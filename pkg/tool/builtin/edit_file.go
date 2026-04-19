package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const editFileToolName = "edit_file"

func init() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    editFileToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: editFileToolName,
				Description: "Make targeted edits to an existing file using search-and-replace. " +
					"Each edit replaces the FIRST occurrence of the old text with the new text.\n\n" +
					"RULES:\n" +
					"- old_text must match EXACTLY (including whitespace and indentation)\n" +
					"- For multiple edits to the same file, make separate calls\n" +
					"- Use read_file first to see exact content including whitespace\n" +
					"- To delete text, use empty new_text\n" +
					"- To insert, use old_text as the anchor point and include it in new_text",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Path to the file to edit.",
						},
						"old_text": map[string]any{
							"type":        "string",
							"description": "The exact text to find and replace. Must match file content exactly.",
						},
						"new_text": map[string]any{
							"type":        "string",
							"description": "The replacement text. Use empty string to delete the matched text.",
						},
					},
					"required": []string{"path", "old_text", "new_text"},
				}),
			},
		},
		Handler:       handleEditFile,
		NeverParallel: true,
	})
}

type editFileArgs struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func handleEditFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args editFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Path == "" {
		return toolErr("path is required"), nil
	}
	if args.OldText == "" {
		return toolErr("old_text is required and cannot be empty"), nil
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErr("File not found: %s", args.Path), nil
		}
		return toolErr("Failed to read file: %v", err), nil
	}

	content := string(data)

	idx := strings.Index(content, args.OldText)
	if idx == -1 {
		trimOld := strings.TrimSpace(args.OldText)
		if trimIdx := strings.Index(content, trimOld); trimIdx != -1 {
			return toolErr("old_text not found exactly, but a trimmed version was found at offset %d. "+
				"Check whitespace and indentation. Use read_file to see exact content.", trimIdx), nil
		}
		return toolErr("old_text not found in %s. Use read_file to verify the exact content.", args.Path), nil
	}

	count := strings.Count(content, args.OldText)
	if count > 1 {
		log.Debug("edit_file: multiple matches, replacing first only",
			"path", args.Path, "matches", count)
	}

	newContent := content[:idx] + args.NewText + content[idx+len(args.OldText):]

	tmpPath := args.Path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(newContent), 0o644); err != nil {
		return toolErr("Failed to write file: %v", err), nil
	}
	if err := os.Rename(tmpPath, args.Path); err != nil {
		_ = os.Remove(tmpPath)
		return toolErr("Failed to rename temp file: %v", err), nil
	}

	oldLines := strings.Count(args.OldText, "\n") + 1
	newLines := strings.Count(args.NewText, "\n") + 1
	delta := newLines - oldLines

	var deltaStr string
	if delta > 0 {
		deltaStr = fmt.Sprintf("+%d lines", delta)
	} else if delta < 0 {
		deltaStr = fmt.Sprintf("%d lines", delta)
	} else {
		deltaStr = "same line count"
	}

	msg := fmt.Sprintf("Edited %s: replaced %d chars with %d chars (%s)", args.Path, len(args.OldText), len(args.NewText), deltaStr)
	if count > 1 {
		msg += fmt.Sprintf(" [note: %d total matches, only first replaced]", count)
	}

	log.Debug("edit_file: completed", "path", args.Path, "old_len", len(args.OldText), "new_len", len(args.NewText))

	return msg, nil
}
