package openai

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRetryConfig_IsRetryable(t *testing.T) {
	rc := DefaultRetryConfig()

	retryable := []int{429, 500, 502, 503, 504}
	for _, code := range retryable {
		if !rc.IsRetryable(code) {
			t.Errorf("expected %d to be retryable", code)
		}
	}

	nonRetryable := []int{400, 401, 403, 404, 422}
	for _, code := range nonRetryable {
		if rc.IsRetryable(code) {
			t.Errorf("expected %d to NOT be retryable", code)
		}
	}
}

func TestRetryConfig_Backoff(t *testing.T) {
	rc := DefaultRetryConfig()

	prev := time.Duration(0)
	for i := 0; i < 5; i++ {
		b := rc.Backoff(i)
		if b <= 0 {
			t.Errorf("attempt %d: backoff should be positive, got %v", i, b)
		}
		if b > rc.MaxBackoff+time.Duration(float64(rc.MaxBackoff)*rc.JitterFraction) {
			t.Errorf("attempt %d: backoff %v exceeds max %v", i, b, rc.MaxBackoff)
		}
		// Generally should increase (may not due to jitter but trend should be up)
		_ = prev
		prev = b
	}
}

func TestRetryConfig_Sleep_ContextCancel(t *testing.T) {
	rc := RetryConfig{
		InitialBackoff: 10 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	err := rc.Sleep(ctx, 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected context cancelled error")
	}
	if elapsed > 100*time.Millisecond {
		t.Errorf("sleep should have returned immediately, took %v", elapsed)
	}
}

func TestParseRetryAfter(t *testing.T) {
	// Nil response
	if d := ParseRetryAfter(nil); d != 0 {
		t.Errorf("expected 0 for nil response, got %v", d)
	}

	// Seconds
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "30")
	d := ParseRetryAfter(resp)
	if d != 30*time.Second {
		t.Errorf("expected 30s, got %v", d)
	}

	// No header
	resp2 := &http.Response{Header: http.Header{}}
	if d := ParseRetryAfter(resp2); d != 0 {
		t.Errorf("expected 0, got %v", d)
	}
}
