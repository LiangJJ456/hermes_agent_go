package web

import (
	"context"
	"encoding/json"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const toolsetWeb = "web"

func init() {
	RegisterTools(registry.Global())
}

// RegisterTools 注册Web工具到全局注册表
func RegisterTools(reg *registry.Registry) {
	webTool := NewWebTool()

	// web_get
	reg.Register(&registry.ToolEntry{
		Name:    "web_get",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_get",
				Description: "发送HTTP GET请求获取网页内容或API数据\n\n" +
					"PARAMETERS:\n" +
					"- url: 目标URL地址（必填）\n" +
					"- headers: 请求头（可选，键值对）\n" +
					"- params: 查询参数（可选，键值对）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "目标URL地址",
						},
						"headers": map[string]any{
							"type":        "object",
							"description": "请求头（键值对）",
						},
						"params": map[string]any{
							"type":        "object",
							"description": "查询参数（键值对）",
						},
					},
					"required": []string{"url"},
				}),
			},
		},
		Handler:       wrapHandler(webTool.Get),
		ParallelSafe:  true,
		MaxResultSize: 100000,
	})

	// web_post
	reg.Register(&registry.ToolEntry{
		Name:    "web_post",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_post",
				Description: "发送HTTP POST请求提交数据\n\n" +
					"PARAMETERS:\n" +
					"- url: 目标URL地址（必填）\n" +
					"- headers: 请求头（可选，键值对）\n" +
					"- body: 请求体数据（可选，JSON对象）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "目标URL地址",
						},
						"headers": map[string]any{
							"type":        "object",
							"description": "请求头（键值对）",
						},
						"body": map[string]any{
							"type":        "object",
							"description": "请求体数据（JSON对象）",
						},
					},
					"required": []string{"url"},
				}),
			},
		},
		Handler:       wrapHandler(webTool.Post),
		ParallelSafe:  true,
		MaxResultSize: 100000,
	})

	// web_scrape
	reg.Register(&registry.ToolEntry{
		Name:    "web_scrape",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_scrape",
				Description: "抓取网页内容并解析指定选择器的内容\n\n" +
					"PARAMETERS:\n" +
					"- url: 目标URL地址（必填）\n" +
					"- selector: CSS选择器（必填，如'body'、'.content'）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "目标URL地址",
						},
						"selector": map[string]any{
							"type":        "string",
							"description": "CSS选择器",
						},
					},
					"required": []string{"url", "selector"},
				}),
			},
		},
		Handler:       wrapHandler(webTool.ScrapeWebPage),
		ParallelSafe:  true,
		MaxResultSize: 200000,
	})

	// web_download
	reg.Register(&registry.ToolEntry{
		Name:    "web_download",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_download",
				Description: "下载文件到指定路径\n\n" +
					"PARAMETERS:\n" +
					"- url: 文件URL地址（必填）\n" +
					"- save_path: 保存路径（必填）",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url": map[string]any{
							"type":        "string",
							"description": "文件URL地址",
						},
						"save_path": map[string]any{
							"type":        "string",
							"description": "保存路径",
						},
					},
					"required": []string{"url", "save_path"},
				}),
			},
		},
		Handler:       wrapHandler(webTool.DownloadFile),
		ParallelSafe:  true,
	})
}

type getArgs struct {
	URL     string                 `json:"url"`
	Headers map[string]string      `json:"headers,omitempty"`
	Params  map[string]string      `json:"params,omitempty"`
}

type postArgs struct {
	URL     string                 `json:"url"`
	Headers map[string]string      `json:"headers,omitempty"`
	Body    map[string]interface{} `json:"body,omitempty"`
}

type scrapeArgs struct {
	URL      string `json:"url"`
	Selector string `json:"selector"`
}

type downloadArgs struct {
	URL      string `json:"url"`
	SavePath string `json:"save_path"`
}

func wrapHandler(fn func(string, map[string]string, interface{}) (map[string]interface{}, error)) registry.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args map[string]interface{}
		if err := json.Unmarshal(raw, &args); err != nil {
			return "无效参数格式: " + err.Error(), nil
		}

		url, ok := args["url"].(string)
		if !ok {
			return "参数url必填且必须为字符串", nil
		}

		headers, _ := args["headers"].(map[string]string)
		body := args["body"]

		result, err := fn(url, headers, body)
		if err != nil {
			return "请求失败: " + err.Error(), nil
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