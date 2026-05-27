package delegate

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/agent"
)

func TestBuildFullTask_TaskOnly(t *testing.T) {
	args := delegateArgs{Task: "do the thing"}
	got := buildFullTask(args)
	if got != "do the thing" {
		t.Fatalf("expected %q, got %q", "do the thing", got)
	}
}

func TestBuildFullTask_WithContextAndConstraints(t *testing.T) {
	args := delegateArgs{
		Task:        "do the thing",
		Context:     "file: main.go",
		Constraints: "no side effects",
	}
	got := buildFullTask(args)
	if !strings.Contains(got, "## Context\nfile: main.go") {
		t.Errorf("missing Context section: %s", got)
	}
	if !strings.Contains(got, "## Constraints\nno side effects") {
		t.Errorf("missing Constraints section: %s", got)
	}
}

func TestBuildTaskNotification_Completed(t *testing.T) {
	stats := agent.Stats{TotalIterations: 3, ToolCalls: 7}
	xml := buildTaskNotification("task-123", "analyze auth", "found bug at line 42", nil, 5*time.Second, stats)

	for _, want := range []string{
		"<task-notification>",
		"<task-id>task-123</task-id>",
		"<status>completed</status>",
		"<task>analyze auth</task>",
		"<iterations>3</iterations>",
		"<tool-calls>7</tool-calls>",
		"found bug at line 42",
		"</task-notification>",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("missing %q in output:\n%s", want, xml)
		}
	}
}

func TestBuildTaskNotification_Failed(t *testing.T) {
	stats := agent.Stats{}
	xml := buildTaskNotification("task-456", "fix bug", "", fmt.Errorf("timeout"), time.Second, stats)

	if !strings.Contains(xml, "<status>failed</status>") {
		t.Errorf("expected failed status:\n%s", xml)
	}
	if !strings.Contains(xml, "timeout") {
		t.Errorf("expected error in result:\n%s", xml)
	}
}
