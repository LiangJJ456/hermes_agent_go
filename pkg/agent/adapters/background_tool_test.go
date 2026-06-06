package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
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

func TestRunStandardTool_FastCompletes(t *testing.T) {
	exec := func(ctx context.Context, res string, args json.RawMessage) (string, error) {
		return "quick-result", nil
	}
	notified := make(chan string, 1)
	notify := func(xml string) { notified <- xml }
	res, err := runStandardTool(context.Background(), context.Background(), 50*time.Millisecond, notify, "t", nil, exec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "quick-result" {
		t.Fatalf("got %v", res.Output)
	}
	select {
	case <-notified:
		t.Fatal("notify must not fire on fast completion")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRunStandardTool_SlowGoesBackground(t *testing.T) {
	exec := func(ctx context.Context, res string, args json.RawMessage) (string, error) {
		time.Sleep(40 * time.Millisecond)
		return "late-result", nil
	}
	notified := make(chan string, 1)
	notify := func(xml string) { notified <- xml }
	res, err := runStandardTool(context.Background(), context.Background(), 5*time.Millisecond, notify, "search", nil, exec)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := res.Output.(string)
	if !strings.Contains(out, "后台") {
		t.Fatalf("expected background placeholder, got %v", res.Output)
	}
	select {
	case xml := <-notified:
		if !strings.Contains(xml, "late-result") {
			t.Fatalf("notify missing real result: %s", xml)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("notify not called after background completion")
	}
}

func TestRunStandardTool_TurnCancel(t *testing.T) {
	started := make(chan struct{})
	exec := func(ctx context.Context, res string, args json.RawMessage) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	_, err := runStandardTool(ctx, context.Background(), time.Hour, func(string) {}, "t", nil, exec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestRunStandardTool_DisabledBlocks(t *testing.T) {
	exec := func(ctx context.Context, res string, args json.RawMessage) (string, error) {
		return "sync", nil
	}
	res, err := runStandardTool(context.Background(), context.Background(), 0, nil, "t", nil, exec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "sync" {
		t.Fatalf("got %v", res.Output)
	}
}
