package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	readFileToolName = "read_file"
	defaultMaxLines  = 2000
)

func init() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    readFileToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: readFileToolName,
				Description: "Read the contents of a file. Output includes line numbers for reference. " +
					"For large files, use offset and limit to read specific portions.\n\n" +
					"TIPS:\n" +
					"- Always check file size first for very large files\n" +
					"- Use offset/limit for targeted reading of large files\n" +
					"- Line numbers in output help with subsequent edit_file calls",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Absolute or relative path to the file.",
						},
						"offset": map[string]any{
							"type":        "integer",
							"description": "Starting line number (1-based). Default: 1.",
						},
						"limit": map[string]any{
							"type":        "integer",
							"description": "Maximum number of lines to return. Default: 2000.",
						},
					},
					"required": []string{"path"},
				}),
			},
		},
		Handler:      handleReadFile,
		ParallelSafe: true,
	})
}

type readFileArgs struct {
	Path   string  `json:"path"`
	Offset float64 `json:"offset,omitempty"`
	Limit  float64 `json:"limit,omitempty"`
}

func handleReadFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Path == "" {
		return toolErr("path is required"), nil
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErr("File not found: %s", args.Path), nil
		}
		return toolErr("Cannot access file: %v", err), nil
	}
	if info.IsDir() {
		return toolErr("Path is a directory, not a file: %s. Use list_dir instead.", args.Path), nil
	}

	data, err := os.ReadFile(args.Path)
	if err != nil {
		return toolErr("Failed to read file: %v", err), nil
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	offset := 1
	if args.Offset > 0 {
		offset = int(args.Offset)
	}
	if offset > totalLines {
		return toolErr("Offset %d exceeds total lines (%d).", offset, totalLines), nil
	}

	limit := defaultMaxLines
	if args.Limit > 0 {
		limit = int(args.Limit)
	}

	startIdx := offset - 1
	endIdx := startIdx + limit
	if endIdx > totalLines {
		endIdx = totalLines
	}

	var sb strings.Builder
	lineNumWidth := len(strconv.Itoa(endIdx))

	for i := startIdx; i < endIdx; i++ {
		lineNum := i + 1
		sb.WriteString(fmt.Sprintf("%*d | %s\n", lineNumWidth, lineNum, lines[i]))
	}

	if startIdx > 0 || endIdx < totalLines {
		sb.WriteString(fmt.Sprintf("\n[Showing lines %d-%d of %d total | file: %s | size: %d bytes]",
			offset, endIdx, totalLines, args.Path, info.Size()))
	} else {
		sb.WriteString(fmt.Sprintf("\n[%d lines | file: %s | size: %d bytes]",
			totalLines, args.Path, info.Size()))
	}

	return sb.String(), nil
}

func toolErr(format string, a ...any) string {
	msg := fmt.Sprintf(format, a...)
	b, _ := json.Marshal(map[string]any{"error": msg})
	return string(b)
}
