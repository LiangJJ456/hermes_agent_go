package mimo

import (
	"context"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	JitterFraction float64
	RetryableCodes map[int]bool
}

func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.2,
		RetryableCodes: map[int]bool{
			http.StatusTooManyRequests:     true,
			http.StatusInternalServerError: true,
			http.StatusBadGateway:          true,
			http.StatusServiceUnavailable:  true,
			http.StatusGatewayTimeout:      true,
		},
	}
}

func (rc *RetryConfig) IsRetryable(statusCode int) bool {
	return rc.RetryableCodes[statusCode]
}

func (rc *RetryConfig) Backoff(attempt int) time.Duration {
	backoff := float64(rc.InitialBackoff) * math.Pow(rc.Multiplier, float64(attempt))
	if backoff > float64(rc.MaxBackoff) {
		backoff = float64(rc.MaxBackoff)
	}
	jitter := backoff * rc.JitterFraction * (rand.Float64()*2 - 1)
	backoff += jitter
	if backoff < 0 {
		backoff = float64(rc.InitialBackoff)
	}
	return time.Duration(backoff)
}

func (rc *RetryConfig) Sleep(ctx context.Context, attempt int) error {
	d := rc.Backoff(attempt)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func ParseRetryAfter(resp *http.Response) time.Duration {
	if resp == nil {
		return 0
	}
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		return 0
	}
	if secs, err := strconv.Atoi(ra); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(ra); err == nil {
		return time.Until(t)
	}
	return 0
}
