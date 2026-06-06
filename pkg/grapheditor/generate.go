package grapheditor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

// ErrNoValidJSON is returned when the model never produced extractable JSON.
var ErrNoValidJSON = errors.New("model did not return valid graph JSON")

// ChatFunc abstracts "call the LLM once, get text back" so the generator is
// testable without a real provider.
type ChatFunc func(ctx context.Context, messages []types.Message) (string, error)

// GraphGenerator turns a natural-language instruction into a validated graph
// via a validate-and-self-correct loop.
type GraphGenerator struct {
	chat     ChatFunc
	schema   []NodeTypeSchema
	maxRetry int
}

// NewGraphGenerator builds a generator. maxRetry is the number of correction
// rounds after the first attempt (clamped to >= 0).
func NewGraphGenerator(chat ChatFunc, schema []NodeTypeSchema, maxRetry int) *GraphGenerator {
	if maxRetry < 0 {
		maxRetry = 0
	}
	return &GraphGenerator{chat: chat, schema: schema, maxRetry: maxRetry}
}

// GenerateResult is the response payload of /api/generate.
type GenerateResult struct {
	Graph    json.RawMessage   `json:"graph"`
	Valid    bool              `json:"valid"`
	Errors   []ValidationError `json:"errors"`
	Attempts int               `json:"attempts"`
	Notes    string            `json:"notes,omitempty"`
}

// Generate runs the loop. current is the current canvas graph JSON (nil/empty
// for from-scratch). It returns the last graph even when validation never
// passes; it only returns an error for chat failures or unextractable JSON.
func (g *GraphGenerator) Generate(ctx context.Context, instruction string, current []byte) (GenerateResult, error) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: g.systemPrompt()},
		{Role: types.RoleUser, Content: userPrompt(instruction, current)},
	}
	var lastJSON json.RawMessage
	var lastErrors []ValidationError
	maxAttempts := 1 + g.maxRetry
	attempts := 0
	for attempts < maxAttempts {
		attempts++
		text, err := g.chat(ctx, messages)
		if err != nil {
			return GenerateResult{}, fmt.Errorf("llm chat: %w", err)
		}
		raw, ok := extractJSON(text)
		if !ok {
			if attempts < maxAttempts {
				messages = append(messages,
					types.Message{Role: types.RoleAssistant, Content: text},
					types.Message{Role: types.RoleUser, Content: "只输出图的 JSON,不要任何解释或 markdown。"})
				continue
			}
			return GenerateResult{}, fmt.Errorf("%w: %s", ErrNoValidJSON, snippet(text))
		}
		vr := ValidateGraph(raw)
		if vr.Valid {
			return GenerateResult{Graph: raw, Valid: true, Errors: []ValidationError{}, Attempts: attempts}, nil
		}
		lastJSON, lastErrors = raw, vr.Errors
		if attempts < maxAttempts {
			messages = append(messages,
				types.Message{Role: types.RoleAssistant, Content: text},
				types.Message{Role: types.RoleUser, Content: "校验未通过,错误如下,请修正后只输出修正后的完整 JSON:\n" + formatErrors(vr.Errors)})
		}
	}
	return GenerateResult{Graph: lastJSON, Valid: false, Errors: lastErrors, Attempts: attempts}, nil
}

func (g *GraphGenerator) systemPrompt() string {
	schemaJSON, _ := json.MarshalIndent(g.schema, "", "  ")
	return strings.Join([]string{
		"你是 Hermes 编排图生成器。根据用户需求输出一张合法的工作流图 JSON。",
		"",
		"可用节点类型与字段(schema):",
		string(schemaJSON),
		"",
		"硬性约束:",
		`- 顶层键用 PascalCase:"StartAt"(起点节点名)、"Nodes"(对象,键为节点名)、"Edges"(数组)。`,
		`- 每个节点形如 {"Type":"<类型>","Config":{...}};Config 的键用 schema 中的字段名(PascalCase)。`,
		`- 每条边形如 {"From":"<节点>","To":"<节点>","Priority":<数字>},可选 "Condition":"<表达式>"。`,
		`- raw 类字段(如 OutputSchema/Parameters/Choices/Branches/Schema)直接放 JSON 值;Tools/Actions 是字符串数组。`,
		"- 必须恰有一个起点;所有边端点必须引用已存在的节点。",
		"- 只输出 JSON 本身,不要 markdown、不要解释文字。",
	}, "\n")
}

func userPrompt(instruction string, current []byte) string {
	if len(strings.TrimSpace(string(current))) > 0 && string(current) != "null" {
		return "在以下现有图的基础上修改(只输出修改后的完整图 JSON):\n" + string(current) +
			"\n\n修改需求:" + instruction
	}
	return "需求:" + instruction
}

func formatErrors(errs []ValidationError) string {
	var b strings.Builder
	for _, e := range errs {
		fmt.Fprintf(&b, "- [%s] %s\n", e.Path, e.Message)
	}
	return b.String()
}

func snippet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200]
	}
	return s
}
