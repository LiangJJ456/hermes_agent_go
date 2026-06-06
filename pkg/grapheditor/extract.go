package grapheditor

import (
	"encoding/json"
	"strings"
)

// extractJSON pulls a JSON object out of an LLM reply. It tries, in order:
// the whole trimmed text, a ```fenced``` block, then a brace-balanced span
// from the first '{'. Returns ok=false when nothing parses as valid JSON.
func extractJSON(text string) (json.RawMessage, bool) {
	s := strings.TrimSpace(text)
	if json.Valid([]byte(s)) {
		return json.RawMessage(s), true
	}
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:] // drop optional language tag line
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			cand := strings.TrimSpace(rest[:j])
			if json.Valid([]byte(cand)) {
				return json.RawMessage(cand), true
			}
		}
	}
	if start := strings.IndexByte(s, '{'); start >= 0 {
		depth := 0
		for i := start; i < len(s); i++ {
			switch s[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					cand := s[start : i+1]
					if json.Valid([]byte(cand)) {
						return json.RawMessage(cand), true
					}
				}
			}
		}
	}
	return nil, false
}
