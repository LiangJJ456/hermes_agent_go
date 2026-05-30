package agent

import (
	"context"
	"testing"
	"time"

	"code.byted.org/ad_creative/hermes_agent_go/pkg/types"
)

func TestAddNotification_StoresAndSignals(t *testing.T) {
	a := &AIAgent{notifCh: make(chan struct{}, 1)}

	a.AddNotification("<task-notification><task-id>t1</task-id></task-notification>")

	if len(a.pendingNotifs) != 1 {
		t.Fatalf("expected 1 pending notif, got %d", len(a.pendingNotifs))
	}
	select {
	case <-a.notifCh:
	case <-time.After(10 * time.Millisecond):
		t.Fatal("expected signal on notifCh, got timeout")
	}
}

func TestAddNotification_MultipleNotifsSingleSignal(t *testing.T) {
	a := &AIAgent{notifCh: make(chan struct{}, 1)}

	a.AddNotification("notif1")
	a.AddNotification("notif2")
	a.AddNotification("notif3")

	if len(a.pendingNotifs) != 3 {
		t.Fatalf("expected 3 notifs, got %d", len(a.pendingNotifs))
	}
	count := 0
	for {
		select {
		case <-a.notifCh:
			count++
		default:
			if count != 1 {
				t.Fatalf("expected 1 signal, got %d", count)
			}
			return
		}
	}
}

func TestNotifCh_ReturnsReadOnly(t *testing.T) {
	a := &AIAgent{notifCh: make(chan struct{}, 1)}
	if a.NotifCh() == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestRun_GuardEmptyInputNoPendingNotifs(t *testing.T) {
	cfg := types.AgentConfig{Model: "test/model", WorkDir: t.TempDir(), MaxDelegateDepth: 2}
	a, err := NewAIAgent(cfg, nil, nil)
	if err != nil {
		t.Fatalf("NewAIAgent: %v", err)
	}
	// Pre-populate messages to skip system-prompt init path
	a.messages = []types.Message{{Role: "system", Content: "sys"}}

	reply, pending, err := a.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pending || reply != "" {
		t.Fatalf("expected empty reply and pending=false, got reply=%q pending=%v", reply, pending)
	}
}

func TestRun_DrainsPendingNotificationsAsMessages(t *testing.T) {
	cfg := types.AgentConfig{Model: "test/model", WorkDir: t.TempDir(), MaxDelegateDepth: 2}
	a, err := NewAIAgent(cfg, nil, nil)
	if err != nil {
		t.Fatalf("NewAIAgent: %v", err)
	}
	a.messages = []types.Message{{Role: "system", Content: "sys"}}

	// Wire mock LLM via executor so the stateless runner reads it from ec
	a.executor.LLMInvoker = &mockLLMInvoker{reply: "ack"}

	notif := "<task-notification><task-id>abc</task-id><status>completed</status><result>done</result></task-notification>"
	a.pendingNotifs = []string{notif}

	reply, _, err := a.Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if reply == "" {
		t.Error("expected non-empty reply when draining notifications")
	}

	a.mu.Lock()
	msgs := make([]types.Message, len(a.messages))
	copy(msgs, a.messages)
	a.mu.Unlock()

	found := false
	for _, m := range msgs {
		if m.Role == types.RoleUser && m.Content == notif {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("notification not injected into messages; got %d messages", len(msgs))
	}
}
