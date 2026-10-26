// Package web 提供基于 Go 内嵌文件系统（go:embed）的 Web 控制台服务，支持本地配置与实时日志监视。
//
// @author Ateng
// @since 2026-10-08
package web

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// LogEntry 单条日志结构体，供前端实时控制台展示。
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// LogBuffer 环形内存日志收集缓冲区，实现 slog.Handler 接口。
type LogBuffer struct {
	maxSize  int
	minLevel slog.Level
	entries  []LogEntry
	mu       sync.RWMutex
}

// NewLogBuffer 创建指定容量的内存日志缓冲区。
func NewLogBuffer(maxSize int) *LogBuffer {
	if maxSize <= 0 {
		maxSize = 200
	}
	return &LogBuffer{
		maxSize:  maxSize,
		minLevel: slog.LevelInfo,
		entries:  make([]LogEntry, 0, maxSize),
	}
}

// SetMinLevel 设置缓冲区允许接收的最低日志级别。
func (b *LogBuffer) SetMinLevel(level slog.Level) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.minLevel = level
}

// Enabled 报告是否启用特定级别的日志记录。
func (b *LogBuffer) Enabled(ctx context.Context, level slog.Level) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return level >= b.minLevel
}

// Handle 将日志记录存入环形缓冲区。
func (b *LogBuffer) Handle(ctx context.Context, r slog.Record) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	entry := LogEntry{
		Time:    r.Time.Format(time.TimeOnly),
		Level:   r.Level.String(),
		Message: r.Message,
	}

	if len(b.entries) >= b.maxSize {
		b.entries = append(b.entries[1:], entry)
	} else {
		b.entries = append(b.entries, entry)
	}

	return nil
}

// WithAttrs 实现 slog.Handler 接口。
func (b *LogBuffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return b
}

// WithGroup 实现 slog.Handler 接口。
func (b *LogBuffer) WithGroup(name string) slog.Handler {
	return b
}

// GetLogs 获取当前缓冲区中的全部日志切片副本。
func (b *LogBuffer) GetLogs() []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if len(b.entries) == 0 {
		return []LogEntry{}
	}

	res := make([]LogEntry, len(b.entries))
	copy(res, b.entries)
	return res
}
