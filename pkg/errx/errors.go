package errx

import (
	"errors"
	"fmt"
)

// Wrap 包装错误并附加消息
func Wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", msg, err)
}

// New 创建新错误
func New(msg string) error {
	return errors.New(msg)
}

var (
	// Agent 引擎错误
	ErrMaxIterationsReached = errors.New("max iterations reached")
	ErrBudgetExhausted      = errors.New("iteration budget exhausted")
	ErrAgentCancelled       = errors.New("agent cancelled by user")

	// 工具错误
	ErrToolNotFound       = errors.New("tool not found")
	ErrToolAlreadyExists  = errors.New("tool already registered")
	ErrToolExecutionFailed = errors.New("tool execution failed")
	ErrToolTimeout        = errors.New("tool execution timeout")

	// 模型错误
	ErrModelNotSupported = errors.New("model not supported")
	ErrAPIKeyMissing     = errors.New("api key not configured")
	ErrRateLimited       = errors.New("rate limited")
	ErrContextTooLong    = errors.New("context length exceeded")

	// 子 Agent 错误
	ErrDelegateDepthExceeded = errors.New("delegate depth exceeded")
	ErrDelegateNotAllowed    = errors.New("delegate not allowed in sub-agent")

	// 状态存储错误
	ErrSessionNotFound = errors.New("session not found")
	ErrDBLocked        = errors.New("database locked after max retries")
)
