package grapheditor

import (
	"testing"

	_ "code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator/runner"
)

func findSchema(schemas []NodeTypeSchema, typ string) *NodeTypeSchema {
	for i := range schemas {
		if schemas[i].Type == typ {
			return &schemas[i]
		}
	}
	return nil
}

func findField(s *NodeTypeSchema, jsonName string) *FieldSchema {
	for i := range s.Fields {
		if s.Fields[i].JSONName == jsonName {
			return &s.Fields[i]
		}
	}
	return nil
}

func TestBuildNodeTypeSchemas_LLM(t *testing.T) {
	schemas := BuildNodeTypeSchemas()

	llm := findSchema(schemas, "llm")
	if llm == nil {
		t.Fatal("llm schema missing")
	}
	if f := findField(llm, "Model"); f == nil || f.Type != "string" {
		t.Fatalf("Model field wrong: %+v", f)
	}
	if f := findField(llm, "Tools"); f == nil || f.Type != "string[]" || !f.Optional {
		t.Fatalf("Tools field wrong: %+v", f)
	}
	if f := findField(llm, "OutputSchema"); f == nil || f.Type != "raw" {
		t.Fatalf("OutputSchema field wrong: %+v", f)
	}
	if f := findField(llm, "Temperature"); f == nil || f.Type != "number" {
		t.Fatalf("Temperature field wrong: %+v", f)
	}
}

func TestBuildNodeTypeSchemas_ToolAndChoice(t *testing.T) {
	schemas := BuildNodeTypeSchemas()

	tool := findSchema(schemas, "tool")
	if tool == nil {
		t.Fatal("tool schema missing")
	}
	if f := findField(tool, "Resource"); f == nil || f.Type != "string" {
		t.Fatalf("Resource field wrong: %+v", f)
	}
	if f := findField(tool, "Async"); f == nil || f.Type != "bool" {
		t.Fatalf("Async field wrong: %+v", f)
	}
	if f := findField(tool, "Timeout"); f == nil || f.Type != "number" {
		t.Fatalf("Timeout field wrong: %+v", f)
	}
	if f := findField(tool, "Parameters"); f == nil || f.Type != "raw" {
		t.Fatalf("Parameters field wrong: %+v", f)
	}

	choice := findSchema(schemas, "choice")
	if choice == nil {
		t.Fatal("choice schema missing")
	}
	if f := findField(choice, "Choices"); f == nil || f.Type != "raw" {
		t.Fatalf("Choices field wrong: %+v", f)
	}
	if f := findField(choice, "Default"); f == nil || f.Type != "string" || !f.Optional {
		t.Fatalf("Default field wrong: %+v", f)
	}
}

func TestParseJSONTag_DivergentName(t *testing.T) {
	// json key differs from Go field name + omitempty present
	name, optional := parseJSONTag("different_name,omitempty", "GoFieldName")
	if name != "different_name" {
		t.Fatalf("name = %q, want %q", name, "different_name")
	}
	if !optional {
		t.Fatal("optional should be true when ,omitempty present")
	}

	// empty tag falls back to the Go field name, not optional
	name, optional = parseJSONTag("", "GoFieldName")
	if name != "GoFieldName" {
		t.Fatalf("empty-tag name = %q, want %q", name, "GoFieldName")
	}
	if optional {
		t.Fatal("optional should be false for empty tag")
	}

	// name omitted but options present (",omitempty") falls back to field name
	name, optional = parseJSONTag(",omitempty", "GoFieldName")
	if name != "GoFieldName" {
		t.Fatalf("optionsonly name = %q, want %q", name, "GoFieldName")
	}
	if !optional {
		t.Fatal("optional should be true for \",omitempty\"")
	}
}

func TestBuildNodeTypeSchemas_RawComplexFields(t *testing.T) {
	schemas := BuildNodeTypeSchemas()

	parallel := findSchema(schemas, "parallel")
	if parallel == nil {
		t.Fatal("parallel schema missing")
	}
	if f := findField(parallel, "Branches"); f == nil || f.Type != "raw" {
		t.Fatalf("Branches field wrong: %+v", f)
	}

	human := findSchema(schemas, "human")
	if human == nil {
		t.Fatal("human schema missing")
	}
	if f := findField(human, "Schema"); f == nil || f.Type != "raw" {
		t.Fatalf("Schema field wrong: %+v", f)
	}
}
