package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/log"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const (
	bashToolName   = "bash"
	toolsetCore    = "core"
	defaultTimeout = 120 * time.Second
	maxOutputBytes = 100_000
)

func init() {
	registry.Global().Register(&registry.ToolEntry{
		Name:    bashToolName,
		Toolset: toolsetCore,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: bashToolName,
				Description: "Execute a bash command in the user's environment. " +
					"Use for: running scripts, installing packages, file operations, " +
					"git commands, compilation, and any system task.\n\n" +
					"GUIDELINES:\n" +
					"- Always quote variable expansions and file paths\n" +
					"- For long-running commands, consider background execution\n" +
					"- Avoid interactive commands (vim, less); use non-interactive alternatives\n" +
					"- Chain commands with && for sequential execution\n" +
					"- Redirect large outputs to files",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{
							"type":        "string",
							"description": "The bash command to execute.",
						},
						"timeout": map[string]any{
							"type":        "integer",
							"description": "Timeout in seconds (default 120, max 1800).",
						},
					},
					"required": []string{"command"},
				}),
			},
		},
		Handler:       handleBash,
		ParallelSafe:  false,
		NeverParallel: false,
		MaxResultSize: maxOutputBytes,
	})
}

type bashArgs struct {
	Command string  `json:"command"`
	Timeout float64 `json:"timeout,omitempty"`
}

func handleBash(ctx context.Context, raw json.RawMessage) (string, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return toolErr("invalid arguments: %v", err), nil
	}
	if args.Command == "" {
		return toolErr("command is required"), nil
	}

	timeout := defaultTimeout
	if args.Timeout > 0 {
		t := args.Timeout
		if t > 1800 {
			t = 1800
		}
		timeout = time.Duration(t) * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	log.Debug("bash: executing", "command", truncateStr(args.Command, 200))

	shell, shellArgs := getShell()
	cmd := exec.CommandContext(ctx, shell, shellArgs, args.Command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	elapsed := time.Since(startTime)

	var sb strings.Builder

	outStr := stdout.String()
	errStr := stderr.String()

	if len(outStr) > maxOutputBytes {
		outStr = outStr[:maxOutputBytes] + fmt.Sprintf("\n... [output truncated at %d bytes]", maxOutputBytes)
	}
	if len(errStr) > maxOutputBytes/2 {
		errStr = errStr[:maxOutputBytes/2] + fmt.Sprintf("\n... [stderr truncated at %d bytes]", maxOutputBytes/2)
	}

	if outStr != "" {
		sb.WriteString(outStr)
	}
	if errStr != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("[stderr]\n")
		sb.WriteString(errStr)
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			sb.WriteString(fmt.Sprintf("\n[timed out after %v]", timeout))
			exitCode = 124
		} else {
			sb.WriteString(fmt.Sprintf("\n[error: %v]", err))
			exitCode = 1
		}
	}

	if exitCode != 0 {
		sb.WriteString(fmt.Sprintf("\n[exit code: %d]", exitCode))
	}

	log.Debug("bash: completed",
		"exit_code", exitCode,
		"elapsed", elapsed.String(),
		"stdout_len", stdout.Len(),
		"stderr_len", stderr.Len())

	result := sb.String()
	if result == "" {
		result = "(no output)"
	}
	return result, nil
}

// getShell detects the best available shell for the current platform.
// On Windows, it looks for bash.exe via PATH, then via Git installation, then falls back to cmd.exe.
// On other platforms, it uses bash.
func getShell() (string, string) {
	if runtime.GOOS != "windows" {
		return "bash", "-c"
	}

	// 1. bash.exe directly in PATH (e.g. MSYS2, Cygwin, or user-added)
	if bashPath, err := exec.LookPath("bash.exe"); err == nil {
		return bashPath, "-c"
	}

	// 2. Find bash.exe relative to git.exe (Git for Windows ships bash in <git>\bin\)
	if gitPath, err := exec.LookPath("git.exe"); err == nil {
		// git.exe is in <git>\cmd\git.exe; bash is in <git>\bin\bash.exe
		gitDir := filepath.Dir(gitPath)                // ...\Git\cmd
		binDir := filepath.Join(gitDir, "..", "bin")   // ...\Git\bin
		bashPath := filepath.Join(binDir, "bash.exe")
		if _, err := os.Stat(bashPath); err == nil {
			return bashPath, "-c"
		}
	}

	// 3. Fallback: cmd.exe
	return "cmd", "/c"
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// mustJSON 将 map 序列化为 json.RawMessage，用于构建 Parameters。
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}
