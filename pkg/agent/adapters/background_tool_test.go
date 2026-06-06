package adapters

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuildToolNotification_Success(t *testing.T) {
	xml := buildToolNotification("web.search", json.RawMessage(`{"q":"x"}`), "found it", nil)
	if !strings.Contains(xml, "web.search") || !strings.Contains(xml, "found it") {
		t.Fatalf("missing fields: %s", xml)
	}
}

func TestBuildToolNotification_Error(t *testing.T) {
	xml := buildToolNotification("t", nil, "", errors.New("boom"))
	if !strings.Contains(xml, "boom") || !strings.Contains(xml, "error") {
		t.Fatalf("missing error: %s", xml)
	}
}
