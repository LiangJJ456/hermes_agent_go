# 终端工具模块使用文档

## 概述
终端工具模块提供了一系列终端交互功能，包括命令执行、交互式会话、输出解析等。

## 工具列表

### 1. terminal_exec - 执行终端命令
**功能**：执行单个终端命令

**参数**：
- `command` (string, 必填)：要执行的命令
- `args` (array, 可选)：命令参数数组

**示例1**：简单命令
```json
{
  "action": "terminal_exec",
  "command": "ls",
  "args": ["-l", "-a"]
}
```

**示例2**：复杂命令
```json
{
  "action": "terminal_exec",
  "command": "grep -r 'TODO' . --include='*.go'"
}
```

**返回结果**：
```json
{
  "command": "ls",
  "args": ["-l", "-a"],
  "stdout": "total 8\n.  ..  file1.go  file2.go",
  "stderr": "",
  "exit_code": 0
}
```

---

### 2. terminal_interactive - 启动交互式终端会话
**功能**：启动交互式终端会话，支持多次命令输入

**参数**：
- `prompt` (string, 可选)：提示符（默认'> '）

**示例**：
```json
{
  "action": "terminal_interactive",
  "prompt": "my-shell> "
}
```

**使用方式**：
1. 输入命令后按回车执行
2. 输入'exit'或'quit'退出会话
3. 每次命令执行后会显示输出

---

### 3. terminal_info - 获取终端信息
**功能**：获取当前终端环境信息

**参数**：无

**示例**：
```json
{
  "action": "terminal_info"
}
```

**返回结果**：
```json
{
  "term": "xterm-256color",
  "shell": "/bin/bash",
  "width": 80,
  "height": 24,
  "cwd": "/Users/user/project",
  "environment": {
    "HOME": "/Users/user",
    "PATH": "/usr/local/bin:/usr/bin:/bin"
  }
}
```

---

### 4. terminal_parse - 解析命令输出
**功能**：解析命令输出为结构化数据

**参数**：
- `output` (string, 必填)：要解析的输出内容
- `parser` (string, 必填)：解析器类型
  - `json`：解析为JSON对象
  - `lines`：按行分割为数组
  - `columns`：按列分割为二维数组

**示例**：
```json
{
  "action": "terminal_parse",
  "output": "name: Alice\nage: 30\nemail: alice@example.com",
  "parser": "lines"
}
```

**返回结果**：
```json
{
  "raw_output": "name: Alice\nage: 30\nemail: alice@example.com",
  "parsed": [
    "name: Alice",
    "age: 30",
    "email: alice@example.com"
  ]
}
```

---

### 5. terminal_script - 执行脚本文件
**功能**：执行本地脚本文件

**参数**：
- `script_path` (string, 必填)：脚本文件路径
- `args` (array, 可选)：脚本参数数组

**示例**：
```json
{
  "action": "terminal_script",
  "script_path": "/Users/user/scripts/deploy.sh",
  "args": ["production", "v1.0.0"]
}
```

---

## 注意事项
1. **安全性**：终端命令执行具有较高权限，避免执行未知来源的命令
2. **超时设置**：命令执行默认超时30秒
3. **输出限制**：标准输出超过100KB会被截断
4. **并发限制**：终端工具不支持并行执行（同一时间只能执行一个终端命令）
5. **交互式会话**：交互式会话会占用终端，退出后才能继续其他操作

## 错误代码
- 1：命令执行失败
- 124：命令执行超时
- 其他：系统返回的退出码