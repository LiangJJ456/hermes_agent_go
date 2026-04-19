package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const listDirToolName = "list_dir"

func init() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    listDirToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: listDirToolName,
				Description: "List the contents of a directory. Returns file/directory names with type indicators " +
					"and sizes. Useful for understanding project structure before reading specific files.\n\n" +
					"OUTPUT FORMAT: Each line shows [type] name (size)\n" +
					"- [DIR] for directories\n" +
					"- [FILE] for regular files",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{
							"type":        "string",
							"description": "Directory path to list. Defaults to current directory.",
						},
						"recursive": map[string]any{
							"type":        "boolean",
							"description": "If true, list recursively (max depth 3). Default: false.",
						},
					},
					"required": []string{"path"},
				}),
			},
		},
		Handler:      handleListDir,
		ParallelSafe: true,
	})
}

type listDirArgs struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive,omitempty"`
}

func handleListDir(_ context.Context, raw json.RawMessage) (string, error) {
	var args listDirArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Path == "" {
		args.Path = "."
	}

	info, err := os.Stat(args.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return toolErr("Directory not found: %s", args.Path), nil
		}
		return toolErr("Cannot access path: %v", err), nil
	}
	if !info.IsDir() {
		return toolErr("Path is a file, not a directory: %s. Use read_file instead.", args.Path), nil
	}

	var sb strings.Builder
	if args.Recursive {
		listRecursive(&sb, args.Path, "", 0, 3)
	} else {
		listFlat(&sb, args.Path)
	}

	result := sb.String()
	if result == "" {
		return fmt.Sprintf("(empty directory: %s)", args.Path), nil
	}
	return result, nil
}

func listFlat(sb *strings.Builder, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		sb.WriteString(fmt.Sprintf("[error reading directory: %v]\n", err))
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if e.Name() == "." || e.Name() == ".." {
			continue
		}
		if e.IsDir() {
			sb.WriteString(fmt.Sprintf("[DIR]  %s/\n", e.Name()))
		} else {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			sb.WriteString(fmt.Sprintf("[FILE] %s (%s)\n", e.Name(), humanSize(size)))
		}
	}
}

func listRecursive(sb *strings.Builder, dir, prefix string, depth, maxDepth int) {
	if depth > maxDepth {
		sb.WriteString(fmt.Sprintf("%s... (max depth %d reached)\n", prefix, maxDepth))
		return
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "vendor": true, ".idea": true, ".vscode": true,
	}

	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." {
			continue
		}

		fullPath := filepath.Join(dir, name)

		if e.IsDir() {
			if skipDirs[name] {
				sb.WriteString(fmt.Sprintf("%s[DIR]  %s/ (skipped)\n", prefix, name))
				continue
			}
			sb.WriteString(fmt.Sprintf("%s[DIR]  %s/\n", prefix, name))
			listRecursive(sb, fullPath, prefix+"  ", depth+1, maxDepth)
		} else {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			sb.WriteString(fmt.Sprintf("%s[FILE] %s (%s)\n", prefix, name, humanSize(size)))
		}
	}
}

func humanSize(bytes int64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1fG", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1fM", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1fK", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}
