package terminal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/errx"
)

// TerminalTool 提供终端相关功能
type TerminalTool struct {
	shell string
}

// NewTerminalTool 创建终端工具实例
func NewTerminalTool() *TerminalTool {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	return &TerminalTool{
		shell: shell,
	}
}

// ExecuteCommand 执行终端命令
func (t *TerminalTool) ExecuteCommand(command string, args ...string) (map[string]interface{}, error) {
	var cmd *exec.Cmd
	
	if len(args) > 0 {
		cmd = exec.Command(command, args...)
	} else {
		// 使用shell执行复杂命令
		cmd = exec.Command(t.shell, "-c", command)
	}

	// 捕获输出和错误
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	
	result := make(map[string]interface{})
	result["command"] = command
	result["args"] = args
	result["stdout"] = stdout.String()
	result["stderr"] = stderr.String()
	result["exit_code"] = 0
	
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result["exit_code"] = exitErr.ExitCode()
		} else {
			return nil, errx.Wrap(err, "failed to execute command")
		}
	}

	return result, nil
}

// InteractiveSession 启动交互式终端会话
func (t *TerminalTool) InteractiveSession(prompt string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	var output []string

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print(prompt)
	
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "exit" || strings.TrimSpace(line) == "quit" {
			break
		}
		
		// 执行命令
		cmdResult, err := t.ExecuteCommand(line)
		if err != nil {
			output = append(output, fmt.Sprintf("Error: %v", err))
		} else {
			output = append(output, cmdResult["stdout"].(string))
			if cmdResult["stderr"].(string) != "" {
				output = append(output, fmt.Sprintf("Stderr: %s", cmdResult["stderr"].(string)))
			}
		}
		
		fmt.Print(prompt)
	}

	if err := scanner.Err(); err != nil {
		return nil, errx.Wrap(err, "failed to read input")
	}

	result["output"] = output
	return result, nil
}

// GetTerminalInfo 获取终端信息
func (t *TerminalTool) GetTerminalInfo() (map[string]interface{}, error) {
	result := make(map[string]interface{})
	
	// 获取终端类型
	result["term"] = os.Getenv("TERM")
	
	// 获取shell路径
	result["shell"] = t.shell
	
	// 获取终端大小
	// 注意：这个功能需要额外的库或系统调用
	// 这里暂时返回默认值
	result["width"] = 80
	result["height"] = 24
	
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err == nil {
		result["cwd"] = cwd
	}

	// 获取环境变量
	env := make(map[string]string)
	for _, e := range os.Environ() {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	result["environment"] = env

	return result, nil
}

// ParseCommandOutput 解析命令输出
func (t *TerminalTool) ParseCommandOutput(output string, parser string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	result["raw_output"] = output
	
	// 根据解析器类型处理输出
	switch strings.ToLower(parser) {
	case "json":
		// 尝试解析为JSON
		var jsonResult interface{}
		if err := json.Unmarshal([]byte(output), &jsonResult); err == nil {
			result["parsed"] = jsonResult
		} else {
			return nil, errx.Wrap(err, "failed to parse JSON")
		}
	case "lines":
		// 按行分割
		lines := strings.Split(output, "\n")
		// 过滤空行
		var nonEmptyLines []string
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				nonEmptyLines = append(nonEmptyLines, line)
			}
		}
		result["parsed"] = nonEmptyLines
	case "columns":
		// 按列分割（适合表格输出）
		lines := strings.Split(output, "\n")
		var parsedLines [][]string
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				// 使用多个空格分割列
				columns := strings.Fields(line)
				parsedLines = append(parsedLines, columns)
			}
		}
		result["parsed"] = parsedLines
	default:
		return nil, errx.New(fmt.Sprintf("unsupported parser type: %s", parser))
	}

	return result, nil
}

// RunScript 执行脚本文件
func (t *TerminalTool) RunScript(scriptPath string, args ...string) (map[string]interface{}, error) {
	// 检查脚本文件是否存在
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return nil, errx.Wrap(err, "script file not found")
	}

	// 使脚本可执行
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return nil, errx.Wrap(err, "failed to make script executable")
	}

	// 执行脚本
	return t.ExecuteCommand(scriptPath, args...)
}