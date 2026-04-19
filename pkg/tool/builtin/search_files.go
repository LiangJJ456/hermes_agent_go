package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	searchFilesToolName = "search_files"
	maxSearchResults    = 300
)

func init() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    searchFilesToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: searchFilesToolName,
				Description: "Search for a pattern in files within a directory. " +
					"Returns matching lines with file paths and line numbers. " +
					"Supports regex patterns.\n\n" +
					"TIPS:\n" +
					"- Use simple strings for exact matches\n" +
					"- Use regex for complex patterns (Go regexp syntax)\n" +
					"- Narrow scope with path and file_pattern to avoid noise",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"pattern": map[string]any{
							"type":        "string",
							"description": "Search pattern (string or regex).",
						},
						"path": map[string]any{
							"type":        "string",
							"description": "Directory to search in. Defaults to current directory.",
						},
						"file_pattern": map[string]any{
							"type":        "string",
							"description": "Glob pattern for file names, e.g. '*.go', '*.py'. Default: all files.",
						},
						"case_sensitive": map[string]any{
							"type":        "boolean",
							"description": "Case-sensitive search. Default: true.",
						},
					},
					"required": []string{"pattern"},
				}),
			},
		},
		Handler:      handleSearchFiles,
		ParallelSafe: true,
	})
}

var (
	searchSkipDirs = map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "vendor": true, ".idea": true, ".vscode": true,
		".mypy_cache": true, ".pytest_cache": true, "dist": true, "build": true,
	}

	binaryExtensions = map[string]bool{
		".exe": true, ".bin": true, ".so": true, ".dylib": true, ".dll": true,
		".o": true, ".a": true, ".pyc": true, ".class": true,
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
		".ico": true, ".svg": true, ".woff": true, ".woff2": true, ".ttf": true,
		".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
		".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	}
)

type searchFilesArgs struct {
	Pattern       string `json:"pattern"`
	Path          string `json:"path,omitempty"`
	FilePattern   string `json:"file_pattern,omitempty"`
	CaseSensitive *bool  `json:"case_sensitive,omitempty"`
}

func handleSearchFiles(_ context.Context, raw json.RawMessage) (string, error) {
	var args searchFilesArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Pattern == "" {
		return toolErr("pattern is required"), nil
	}

	dir := args.Path
	if dir == "" {
		dir = "."
	}

	caseSensitive := true
	if args.CaseSensitive != nil {
		caseSensitive = *args.CaseSensitive
	}

	rePattern := args.Pattern
	if !caseSensitive {
		rePattern = "(?i)" + rePattern
	}
	re, err := regexp.Compile(rePattern)
	if err != nil {
		re = regexp.MustCompile(regexp.QuoteMeta(args.Pattern))
		if !caseSensitive {
			re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(args.Pattern))
		}
	}

	type matchResult struct {
		file    string
		lineNum int
		line    string
	}

	var matches []matchResult
	truncated := false

	walkErr := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			base := filepath.Base(path)
			if searchSkipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if binaryExtensions[ext] {
			return nil
		}

		if args.FilePattern != "" {
			matched, _ := filepath.Match(args.FilePattern, filepath.Base(path))
			if !matched {
				return nil
			}
		}

		if info.Size() > 5*1024*1024 {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		lineNum := 0

		for scanner.Scan() {
			lineNum++
			line := scanner.Text()

			if re.MatchString(line) {
				displayLine := line
				if len(displayLine) > 200 {
					displayLine = displayLine[:200] + "..."
				}

				matches = append(matches, matchResult{
					file:    path,
					lineNum: lineNum,
					line:    displayLine,
				})

				if len(matches) >= maxSearchResults {
					truncated = true
					return fmt.Errorf("limit reached")
				}
			}
		}

		return nil
	})

	if walkErr != nil && !truncated {
		return toolErr("Search error: %v", walkErr), nil
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No matches found for '%s' in %s", args.Pattern, dir), nil
	}

	var sb strings.Builder
	currentFile := ""

	for _, m := range matches {
		if m.file != currentFile {
			if currentFile != "" {
				sb.WriteString("\n")
			}
			sb.WriteString(fmt.Sprintf("── %s ──\n", m.file))
			currentFile = m.file
		}
		sb.WriteString(fmt.Sprintf("%4d | %s\n", m.lineNum, m.line))
	}

	sb.WriteString(fmt.Sprintf("\n[%d matches", len(matches)))
	if truncated {
		sb.WriteString(fmt.Sprintf(", showing first %d", maxSearchResults))
	}
	sb.WriteString("]")

	return sb.String(), nil
}
