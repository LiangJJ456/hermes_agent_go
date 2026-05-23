# Web工具模块使用文档

## 概述
Web工具模块提供了一系列HTTP请求和网页处理功能，包括GET/POST请求、网页抓取、文件下载等。

## 工具列表

### 1. web_get - 发送HTTP GET请求
**功能**：获取网页内容或API数据

**参数**：
- `url` (string, 必填)：目标URL地址
- `headers` (object, 可选)：请求头（键值对）
- `params` (object, 可选)：查询参数（键值对）

**示例**：
```json
{
  "action": "web_get",
  "url": "https://api.github.com/users/LiangJJ456",
  "headers": {
    "Accept": "application/json"
  },
  "params": {
    "per_page": 10
  }
}
```

**返回结果**：
```json
{
  "status_code": 200,
  "status": "200 OK",
  "headers": {...},
  "body": "...",
  "json": {...}
}
```

---

### 2. web_post - 发送HTTP POST请求
**功能**：提交数据到API或表单

**参数**：
- `url` (string, 必填)：目标URL地址
- `headers` (object, 可选)：请求头（键值对）
- `body` (object, 可选)：请求体数据（JSON对象）

**示例**：
```json
{
  "action": "web_post",
  "url": "https://api.example.com/login",
  "headers": {
    "Content-Type": "application/json"
  },
  "body": {
    "username": "admin",
    "password": "123456"
  }
}
```

---

### 3. web_scrape - 抓取网页内容
**功能**：抓取网页内容并解析指定选择器的内容

**参数**：
- `url` (string, 必填)：目标URL地址
- `selector` (string, 必填)：CSS选择器（如'body'、'.content'、'h1'）

**示例**：
```json
{
  "action": "web_scrape",
  "url": "https://example.com",
  "selector": "h1"
}
```

**返回结果**：
```json
{
  "url": "https://example.com",
  "title": "Example Domain",
  "content": ["Example Domain"],
  "links": ["https://www.iana.org/domains/example"]
}
```

---

### 4. web_download - 下载文件
**功能**：下载文件到指定路径

**参数**：
- `url` (string, 必填)：文件URL地址
- `save_path` (string, 必填)：保存路径

**示例**：
```json
{
  "action": "web_download",
  "url": "https://example.com/file.pdf",
  "save_path": "/Users/user/Downloads/file.pdf"
}
```

---

## 注意事项
1. **超时设置**：所有请求默认超时30秒
2. **错误处理**：非200状态码会返回错误信息
3. **内容大小**：返回结果超过100KB会被截断
4. **并发限制**：Web工具支持并行执行（最多8个并发请求）

## 错误代码
- 400：无效参数
- 404：资源未找到
- 500：服务器错误
- 503：服务不可用
- 超时错误：请求超时