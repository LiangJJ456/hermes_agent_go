package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/errx"
)

// WebTool 提供Web相关功能
 type WebTool struct {
	client *http.Client
}

// NewWebTool 创建Web工具实例
func NewWebTool() *WebTool {
	return &WebTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
}

// HTTPRequest 发送HTTP请求
func (w *WebTool) HTTPRequest(method, urlStr string, headers map[string]string, body interface{}) (map[string]interface{}, error) {
	var bodyReader io.Reader
	
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, errx.Wrap(err, "failed to marshal request body")
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequest(method, urlStr, bodyReader)
	if err != nil {
		return nil, errx.Wrap(err, "failed to create request")
	}

	// 设置默认Header
	req.Header.Set("User-Agent", "HermesAgent/1.0 (+https://github.com/LiangJJ456/hermes_agent_go)")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	
	// 添加自定义Header
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, errx.Wrap(err, "failed to send request")
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errx.Wrap(err, "failed to read response body")
	}

	// 解析响应
	result := make(map[string]interface{})
	result["status_code"] = resp.StatusCode
	result["status"] = resp.Status
	result["headers"] = resp.Header
	result["body"] = string(respBody)

	// 尝试解析JSON
	var jsonResult interface{}
	if err := json.Unmarshal(respBody, &jsonResult); err == nil {
		result["json"] = jsonResult
	}

	return result, nil
}

// Get 发送GET请求
func (w *WebTool) Get(urlStr string, headers map[string]string, params map[string]string) (map[string]interface{}, error) {
	// 添加查询参数
	if params != nil && len(params) > 0 {
		parsedURL, err := url.Parse(urlStr)
		if err != nil {
			return nil, errx.Wrap(err, "failed to parse URL")
		}
		
		q := parsedURL.Query()
		for key, value := range params {
			q.Set(key, value)
		}
		parsedURL.RawQuery = q.Encode()
		urlStr = parsedURL.String()
	}

	return w.HTTPRequest("GET", urlStr, headers, nil)
}

// Post 发送POST请求
func (w *WebTool) Post(urlStr string, headers map[string]string, body interface{}) (map[string]interface{}, error) {
	return w.HTTPRequest("POST", urlStr, headers, body)
}

// ScrapeWebPage 抓取网页内容并解析
func (w *WebTool) ScrapeWebPage(urlStr string, selector string) (map[string]interface{}, error) {
	resp, err := w.Get(urlStr, nil, nil)
	if err != nil {
		return nil, err
	}

	statusCode, _ := resp["status_code"].(int)
	if statusCode < 200 || statusCode >= 300 {
		return nil, errx.New(fmt.Sprintf("request failed with status %d", statusCode))
	}

	bodyStr, ok := resp["body"].(string)
	if !ok {
		return nil, errx.New("response body is not a string")
	}

	// 使用goquery解析HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		return nil, errx.Wrap(err, "failed to parse HTML")
	}

	result := make(map[string]interface{})
	result["url"] = urlStr
	result["title"] = doc.Find("title").Text()
	
	// 提取指定选择器的内容
	var content []string
	doc.Find(selector).Each(func(i int, s *goquery.Selection) {
		content = append(content, s.Text())
	})
	result["content"] = content

	// 提取所有链接
	var links []string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		link, exists := s.Attr("href")
		if exists {
			// 转换为绝对URL
			absoluteURL, err := url.Parse(link)
			if err == nil && !absoluteURL.IsAbs() {
				baseURL, _ := url.Parse(urlStr)
				absoluteURL = baseURL.ResolveReference(absoluteURL)
				link = absoluteURL.String()
			}
			links = append(links, link)
		}
	})
	result["links"] = links

	return result, nil
}

// DownloadFile downloads a file and saves it to the specified path.
func (w *WebTool) DownloadFile(urlStr string, savePath string) (map[string]interface{}, error) {
	resp, err := w.client.Get(urlStr)
	if err != nil {
		return nil, errx.Wrap(err, "failed to download file")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errx.New(fmt.Sprintf("download failed with status %d", resp.StatusCode))
	}

	f, err := os.Create(savePath)
	if err != nil {
		return nil, errx.Wrap(err, "failed to create file")
	}
	defer f.Close()

	written, err := io.Copy(f, resp.Body)
	if err != nil {
		return nil, errx.Wrap(err, "failed to write file")
	}

	result := make(map[string]interface{})
	result["url"] = urlStr
	result["save_path"] = savePath
	result["status"] = "success"
	result["bytes_written"] = written
	result["content_length"] = resp.ContentLength

	return result, nil
}