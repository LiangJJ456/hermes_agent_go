package terminal

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const toolsetTerminal = "terminal"

func init() {
	RegisterTools(registry.Global())
}

// RegisterTools registers terminal tools into the global registry.
func RegisterTools(reg *registry.Registry) {
	t := NewTerminalTool()

	reg.Register(&registry.ToolEntry{
		Name:    "terminal_exec",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_exec",
				Description: "Execute a terminal command.\n\n" +
					"PARAMETERS:\n" +
					"- command: The command to execute (required)\n" +
					"- args: Command arguments (optional, array of strings)",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{"type": "string", "description": "Command to execute"},
						"args":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Command arguments"},
					},
					"required": []string{"command"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			command, _ := args["command"].(string)
			var cmdArgs []string
			if arr, ok := args["args"].([]interface{}); ok {
				for _, a := range arr {
					if s, ok := a.(string); ok {
						cmdArgs = append(cmdArgs, s)
					}
				}
			}
			return t.ExecuteCommand(command, cmdArgs...)
		}),
		ParallelSafe:  false,
		MaxResultSize: 100000,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "terminal_interactive",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_interactive",
				Description: "Start an interactive terminal session. Type 'exit' or 'quit' to exit.\n\n" +
					"PARAMETERS:\n" +
					"- prompt: Prompt string (optional, default '> ')",
				Parameters: mustJSON(map[string]any{
					"type":       "object",
					"properties": map[string]any{"prompt": map[string]any{"type": "string", "description": "Prompt string"}},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			prompt, _ := args["prompt"].(string)
			return t.InteractiveSession(prompt)
		}),
		ParallelSafe:  false,
		NeverParallel: true,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "terminal_info",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name:        "terminal_info",
				Description: "Get terminal information (shell, cwd, environment).",
				Parameters:  mustJSON(map[string]any{"type": "object", "properties": map[string]any{}}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			return t.GetTerminalInfo()
		}),
		ParallelSafe: true,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "terminal_parse",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_parse",
				Description: "Parse command output.\n\n" +
					"PARAMETERS:\n" +
					"- output: The output content to parse (required)\n" +
					"- parser: Parser type (required, one of: json, lines, columns)",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"output": map[string]any{"type": "string", "description": "Output content to parse"},
						"parser": map[string]any{"type": "string", "enum": []string{"json", "lines", "columns"}, "description": "Parser type"},
					},
					"required": []string{"output", "parser"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			output, _ := args["output"].(string)
			parser, _ := args["parser"].(string)
			return t.ParseCommandOutput(output, parser)
		}),
		ParallelSafe: true,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "terminal_script",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_script",
				Description: "Execute a script file.\n\n" +
					"PARAMETERS:\n" +
					"- script_path: Path to the script file (required)\n" +
					"- args: Script arguments (optional, array of strings)",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"script_path": map[string]any{"type": "string", "description": "Script file path"},
						"args":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Script arguments"},
					},
					"required": []string{"script_path"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			scriptPath, _ := args["script_path"].(string)
			var scriptArgs []string
			if arr, ok := args["args"].([]interface{}); ok {
				for _, a := range arr {
					if s, ok := a.(string); ok {
						scriptArgs = append(scriptArgs, s)
					}
				}
			}
			return t.RunScript(scriptPath, scriptArgs...)
		}),
		ParallelSafe: false,
	})
}

func wrapHandler(fn func(map[string]interface{}) (map[string]interface{}, error)) registry.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args map[string]interface{}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "invalid arguments: " + err.Error(), nil
		}

		result, err := fn(args)
		if err != nil {
			return "execution failed: " + err.Error(), nil
		}

		jsonResult, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "result serialization failed: " + err.Error(), nil
		}

		return string(jsonResult), nil
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}
