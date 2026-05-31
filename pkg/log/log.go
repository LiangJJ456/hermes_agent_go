package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	logger   *slog.Logger
	levelVar = new(slog.LevelVar) // dynamic level (zero value = LevelInfo)
	once     sync.Once
)

// Init 初始化日志，logDir 为空则仅输出 stderr。
// level 通过 levelVar 应用：即使 logger 已被更早的(惰性)Init 构造，本次设置的
// level 仍会立即生效，因此不受包 init 顺序影响（避免某个 init 先以 Info 锁死级别）。
func Init(logDir string, level slog.Level) {
	levelVar.Set(level)
	once.Do(func() {
		var w io.Writer = os.Stderr
		if logDir != "" {
			_ = os.MkdirAll(logDir, 0o755)
			f, err := os.OpenFile(
				filepath.Join(logDir, "hermes.log"),
				os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644,
			)
			if err == nil {
				w = io.MultiWriter(os.Stderr, f)
			}
		}
		logger = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: levelVar}))
		slog.SetDefault(logger)
	})
}

// InitWithHandler 使用自定义 Handler 初始化（支持 TracedHandler 等 wrapper）
func InitWithHandler(h slog.Handler) {
	logger = slog.New(h)
	slog.SetDefault(logger)
}

// L 返回全局 logger
func L() *slog.Logger {
	if logger == nil {
		Init("", slog.LevelInfo)
	}
	return logger
}

// Debug/Info/Warn/Error 快捷方法
func Debug(msg string, args ...any) { L().Debug(msg, args...) }
func Info(msg string, args ...any)  { L().Info(msg, args...) }
func Warn(msg string, args ...any)  { L().Warn(msg, args...) }
func Error(msg string, args ...any) { L().Error(msg, args...) }

// DebugContext/InfoContext 支持从 ctx 提取 trace 信息
func DebugContext(ctx context.Context, msg string, args ...any) { L().DebugContext(ctx, msg, args...) }
func InfoContext(ctx context.Context, msg string, args ...any)  { L().InfoContext(ctx, msg, args...) }
func WarnContext(ctx context.Context, msg string, args ...any)  { L().WarnContext(ctx, msg, args...) }
func ErrorContext(ctx context.Context, msg string, args ...any) { L().ErrorContext(ctx, msg, args...) }

// Fatalf 打印后退出
func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}
