package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTerminalTool(t *testing.T) {
	tool := NewTerminalTool()
	assert.NotNil(t, tool)
	assert.NotEmpty(t, tool.shell)
}

func TestExecuteCommand(t *testing.T) {
	tool := NewTerminalTool()
	
	// 测试简单命令
	result, err := tool.ExecuteCommand("echo", "hello world")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result["exit_code"])
	assert.Equal(t, "hello world\n", result["stdout"])
	assert.Empty(t, result["stderr"])
	
	// 测试复杂命令
	result, err = tool.ExecuteCommand("ls -l | wc -l")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result["exit_code"])
}

func TestParseCommandOutput(t *testing.T) {
	tool := NewTerminalTool()
	
	// 测试JSON解析
	jsonOutput := `{"name": "test", "value": 123}`
	result, err := tool.ParseCommandOutput(jsonOutput, "json")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	
	parsed := result["parsed"].(map[string]interface{})
	assert.Equal(t, "test", parsed["name"])
	assert.Equal(t, 123.0, parsed["value"])
	
	// 测试行解析
	lineOutput := "line1\nline2\nline3"
	result, err = tool.ParseCommandOutput(lineOutput, "lines")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	
	lines := result["parsed"].([]string)
	assert.Len(t, lines, 3)
	assert.Equal(t, "line1", lines[0])
	assert.Equal(t, "line2", lines[1])
	assert.Equal(t, "line3", lines[2])
}

func TestGetTerminalInfo(t *testing.T) {
	tool := NewTerminalTool()
	
	result, err := tool.GetTerminalInfo()
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotEmpty(t, result["term"])
	assert.NotEmpty(t, result["shell"])
	assert.NotEmpty(t, result["cwd"])
	assert.NotNil(t, result["environment"])
}