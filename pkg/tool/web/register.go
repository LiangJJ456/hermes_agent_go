package web

import (
	"context"
	"encoding/json"
	"fmt"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/tool/registry"
	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

const toolsetWeb = "web"

func init() {
	RegisterTools(registry.Global())
}

// RegisterTools registers web tools into the global registry.
func RegisterTools(reg *registry.Registry) {
	webTool := NewWebTool()

	reg.Register(&registry.ToolEntry{
		Name:    "web_get",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_get",
				Description: "Send HTTP GET request to fetch web content or API data.\n\n" +
					"PARAMETERS:\n" +
					"- url: Target URL (required)\n" +
					"- headers: Request headers (optional, key-value pairs)\n" +
					"- params: Query parameters (optional, key-value pairs)",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":     map[string]any{"type": "string", "description": "Target URL"},
						"headers": map[string]any{"type": "object", "description": "Request headers (key-value pairs)"},
						"params":  map[string]any{"type": "object", "description": "Query parameters (key-value pairs)"},
					},
					"required": []string{"url"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			urlStr, _ := args["url"].(string)
			headers := toStringMap(args["headers"])
			params := toStringMap(args["params"])
			return webTool.Get(urlStr, headers, params)
		}),
		ParallelSafe:  true,
		MaxResultSize: 100000,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "web_post",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_post",
				Description: "Send HTTP POST request to submit data.\n\n" +
					"PARAMETERS:\n" +
					"- url: Target URL (required)\n" +
					"- headers: Request headers (optional, key-value pairs)\n" +
					"- body: Request body (optional, JSON object)",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":     map[string]any{"type": "string", "description": "Target URL"},
						"headers": map[string]any{"type": "object", "description": "Request headers (key-value pairs)"},
						"body":    map[string]any{"type": "object", "description": "Request body (JSON object)"},
					},
					"required": []string{"url"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			urlStr, _ := args["url"].(string)
			headers := toStringMap(args["headers"])
			return webTool.Post(urlStr, headers, args["body"])
		}),
		ParallelSafe:  true,
		MaxResultSize: 100000,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "web_scrape",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_scrape",
				Description: "Scrape web page content using a CSS selector.\n\n" +
					"PARAMETERS:\n" +
					"- url: Target URL (required)\n" +
					"- selector: CSS selector (required, e.g. 'body', '.content')",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":      map[string]any{"type": "string", "description": "Target URL"},
						"selector": map[string]any{"type": "string", "description": "CSS selector"},
					},
					"required": []string{"url", "selector"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			urlStr, _ := args["url"].(string)
			selector, _ := args["selector"].(string)
			return webTool.ScrapeWebPage(urlStr, selector)
		}),
		ParallelSafe:  true,
		MaxResultSize: 200000,
	})

	reg.Register(&registry.ToolEntry{
		Name:    "web_download",
		Toolset: toolsetWeb,
		Schema: types.ToolSchema{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_download",
				Description: "Download a file to the specified path.\n\n" +
					"PARAMETERS:\n" +
					"- url: File URL (required)\n" +
					"- save_path: Save path (required)",
				Parameters: mustJSON(map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":       map[string]any{"type": "string", "description": "File URL"},
						"save_path": map[string]any{"type": "string", "description": "Save path"},
					},
					"required": []string{"url", "save_path"},
				}),
			},
		},
		Handler: wrapHandler(func(args map[string]interface{}) (map[string]interface{}, error) {
			urlStr, _ := args["url"].(string)
			savePath, _ := args["save_path"].(string)
			return webTool.DownloadFile(urlStr, savePath)
		}),
		ParallelSafe: true,
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
			return "request failed: " + err.Error(), nil
		}

		jsonResult, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "result serialization failed: " + err.Error(), nil
		}

		return string(jsonResult), nil
	}
}

func toStringMap(v interface{}) map[string]string {
	raw, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	m := make(map[string]string, len(raw))
	for k, val := range raw {
		if s, ok := val.(string); ok {
			m[k] = s
		}
	}
	return m
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return b
}
