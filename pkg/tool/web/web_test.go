package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewWebTool(t *testing.T) {
	tool := NewWebTool()
	assert.NotNil(t, tool)
	assert.NotNil(t, tool.client)
	assert.Equal(t, 30, int(tool.client.Timeout.Seconds()))
}

func TestHTTPRequest(t *testing.T) {
	tool := NewWebTool()
	
	// 测试GET请求
	result, err := tool.Get("https://httpbin.org/get", nil, map[string]string{"foo": "bar"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 200, result["status_code"])
	
	// 测试POST请求
	postData := map[string]string{"username": "test", "password": "123456"}
	result, err = tool.Post("https://httpbin.org/post", nil, postData)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 200, result["status_code"])
}

func TestScrapeWebPage(t *testing.T) {
	tool := NewWebTool()
	
	// 测试抓取网页
	result, err := tool.ScrapeWebPage("https://example.com", "h1")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "https://example.com", result["url"])
	assert.Equal(t, "Example Domain", result["title"])
	
	content := result["content"].([]string)
	assert.GreaterOrEqual(t, len(content), 1)
	assert.Equal(t, "Example Domain", content[0])
}

func TestDownloadFile(t *testing.T) {
	tool := NewWebTool()
	
	// 测试下载文件
	result, err := tool.DownloadFile("https://httpbin.org/image/png", "/tmp/test.png")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "https://httpbin.org/image/png", result["url"])
	assert.Equal(t, "/tmp/test.png", result["save_path"])
	assert.Equal(t, "success", result["status"])
}