package terminal

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const toolsetTerminal = "terminal"

func init() {
	RegisterTools(registry.Global())
}

// RegisterTools 注册终端工具到全局注册表
func RegisterTools(reg *registry.Registry) {
	terminalTool := NewTerminalTool()

	// terminal_exec
	reg.Register(&registry.ToolEntry{
		Name:    "terminal_exec",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_exec",
				Description: "执行终端命令\n\n" +
					"PARAMETERS:\n" +
					"- command: 要执行的命令（必填）\n" +
					"- args: 命令参数（可选，数组）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command": map[string]any{
							"type":        "string",
							"description": "要执行的命令",
						},
						"args": map[string]any{
							"type":        "array",
							"items": map[string]any{"type": "string"},
							"description": "命令参数",
						},
					},
					"required": []string{"command"},
				}),
			},
		},
		Handler:       wrapHandler(terminalTool.ExecuteCommand),
		ParallelSafe:  false,
		MaxResultSize: 100000,
	})

	// terminal_interactive
	reg.Register(&registry.ToolEntry{
		Name:    "terminal_interactive",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_interactive",
				Description: "启动交互式终端会话，输入'exit'或'quit'退出\n\n" +
					"PARAMETERS:\n" +
					"- prompt: 提示符（可选，默认'> '）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"prompt": map[string]any{
							"type":        "string",
							"description": "提示符",
						},
					},
				}),
			},
		},
		Handler:       wrapHandler(terminalTool.InteractiveSession),
		ParallelSafe:  false,
		NeverParallel: true,
	})

	// terminal_info
	reg.Register(&registry.ToolEntry{
		Name:    "terminal_info",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_info",
				Description: "获取终端信息",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{},
				}),
			},
		},
		Handler:       wrapHandler(terminalTool.GetTerminalInfo),
		ParallelSafe:  true,
	})

	// terminal_parse
	reg.Register(&registry.ToolEntry{
		Name:    "terminal_parse",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_parse",
				Description: "解析命令输出\n\n" +
					"PARAMETERS:\n" +
					"- output: 要解析的输出内容（必填）\n" +
					"- parser: 解析器类型（必填，可选值: json, lines, columns）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"output": map[string]any{
							"type":        "string",
							"description": "要解析的输出内容",
						},
						"parser": map[string]any{
							"type":        "string",
							"enum":        []string{"json", "lines", "columns"},
							"description": "解析器类型",
						},
					},
					"required": []string{"output", "parser"},
				}),
			},
		},
		Handler:       wrapHandler(terminalTool.ParseCommandOutput),
		ParallelSafe:  true,
	})

	// terminal_script
	reg.Register(&registry.ToolEntry{
		Name:    "terminal_script",
		Toolset: toolsetTerminal,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "terminal_script",
				Description: "执行脚本文件\n\n" +
					"PARAMETERS:\n" +
					"- script_path: 脚本文件路径（必填）\n" +
					"- args: 脚本参数（可选，数组）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"script_path": map[string]any{
							"type":        "string",
							"description": "脚本文件路径",
						},
						"args": map[string]any{
							"type":        "array",
							"items": map[string]any{"type": "string"},
							"description": "脚本参数",
						},
					},
					"required": []string{"script_path"},
				}),
			},
		},
		Handler:       wrapHandler(terminalTool.RunScript),
		ParallelSafe:  false,
	})
}

type execArgs struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type interactiveArgs struct {
	Prompt string `json:"prompt,omitempty"`
}

type parseArgs struct {
	Output string `json:"output"`
	Parser string `json:"parser"`
}

type scriptArgs struct {
	ScriptPath string   `json:"script_path"`
	Args       []string `json:"args,omitempty"`
}

func wrapHandler(fn func(interface{}) (map[string]interface{}, error)) registry.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args map[string]interface{}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "无效参数格式: " + err.Error(), nil
		}

		result, err := fn(args)
		if err != nil {
			return "执行失败: " + err.Error(), nil
		}

		jsonResult, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "结果序列化失败: " + err.Error(), nil
		}

		return string(jsonResult), nil
	}
}

// mustJSON 将 map 序列化为 json.RawMessage
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}