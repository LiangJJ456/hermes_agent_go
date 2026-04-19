package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	logger *slog.Logger
	once   sync.Once
)

// Init 初始化日志，logDir 为空则仅输出 stderr
func Init(logDir string, level slog.Level) {
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
		logger = slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
		slog.SetDefault(logger)
	})
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

// Fatalf 打印后退出
func Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "FATAL: "+format+"\n", args...)
	os.Exit(1)
}
