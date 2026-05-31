package grapheditor

import (
	"reflect"
	"strings"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/orchestrator"
)

// NodeTypeSchema describes one registered node type for the editor.
type NodeTypeSchema struct {
	Type   string        `json:"type"`
	Fields []FieldSchema `json:"fields"`
}

// FieldSchema describes one config field. Type is one of:
// "string", "number", "bool", "string[]", "raw". Complex/nested fields
// (nested graphs, maps, json.RawMessage, interface{}, struct slices) are "raw":
// the frontend edits them with a raw-JSON textbox.
type FieldSchema struct {
	Name     string `json:"name"`     // Go field name
	JSONName string `json:"jsonName"` // json tag name (without ,omitempty)
	Type     string `json:"type"`
	Optional bool   `json:"optional"` // json tag has ,omitempty
}

// BuildNodeTypeSchemas reflects every registered node type's config prototype
// into a schema, sorted by type name (ListNodeTypes is sorted).
func BuildNodeTypeSchemas() []NodeTypeSchema {
	names := orchestrator.ListNodeTypes()
	out := make([]NodeTypeSchema, 0, len(names))
	for _, name := range names {
		entry, ok := orchestrator.LookupNodeType(name)
		if !ok {
			continue
		}
		out = append(out, NodeTypeSchema{
			Type:   name,
			Fields: fieldsOf(entry.ConfigPrototype),
		})
	}
	return out
}

func fieldsOf(proto interface{}) []FieldSchema {
	fields := []FieldSchema{}
	if proto == nil {
		return fields
	}
	t := reflect.TypeOf(proto)
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, optional := parseJSONTag(tag, f.Name)
		fields = append(fields, FieldSchema{
			Name:     f.Name,
			JSONName: name,
			Type:     fieldType(f.Type),
			Optional: optional,
		})
	}
	return fields
}

func parseJSONTag(tag, fieldName string) (name string, optional bool) {
	if tag == "" {
		return fieldName, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fieldName
	}
	for _, p := range parts[1:] {
		if p == "omitempty" {
			optional = true
		}
	}
	return name, optional
}

func fieldType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.String {
			return "string[]"
		}
		return "raw"
	default:
		return "raw"
	}
}
