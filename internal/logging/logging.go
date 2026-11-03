// Package logging 提供支持按大小与时间自动轮转的本地文件日志模块。
//
// 日志基于标准库 log/slog 输出，默认仅记录 INFO 及以上级别；同时支持按单文件
// 大小上限与时间周期两种维度轮转，并可限制历史日志的保留数量与保留天数。
//
// @author Ateng
// @since 2026-11-03
package logging

import (
	"io"
	"log/slog"
	"strings"
	"time"
)

const (
	// DefaultDir 默认日志目录。
	DefaultDir = "logs"
	// DefaultFileName 默认活动日志文件名。
	DefaultFileName = "sxd.log"
	// DefaultMaxSizeMB 默认单文件大小上限 (MB)。
	DefaultMaxSizeMB = 10
	// DefaultRotateInterval 默认时间轮转周期。
	DefaultRotateInterval = 24 * time.Hour
	// DefaultMaxBackups 默认保留的历史日志文件数量。
	DefaultMaxBackups = 10
	// DefaultMaxAge 默认历史日志最长保留时间。
	DefaultMaxAge = 30 * 24 * time.Hour
)

// Config 本地文件日志运行时配置。
type Config struct {
	// Dir 日志存放目录。
	Dir string
	// FileName 当前正在写入的活动日志文件名。
	FileName string
	// Level 写入文件的最低日志级别，零值为 INFO。
	Level slog.Level
	// MaxSizeBytes 单文件大小上限 (字节)，超过后立即轮转。
	MaxSizeBytes int64
	// RotateInterval 时间轮转周期，小于等于 0 表示关闭时间轮转。
	RotateInterval time.Duration
	// MaxBackups 最多保留的历史日志文件数量，小于等于 0 表示不限制。
	MaxBackups int
	// MaxAge 历史日志最长保留时间，小于等于 0 表示不限制。
	MaxAge time.Duration
	// JSON 为 true 时使用 JSON 格式输出，否则使用文本格式。
	JSON bool
}

// normalize 补齐空配置的默认值，并清理非法的负值。
func (c Config) normalize() Config {
	if c.Dir == "" {
		c.Dir = DefaultDir
	}
	if c.FileName == "" {
		c.FileName = DefaultFileName
	}
	if c.MaxSizeBytes <= 0 {
		c.MaxSizeBytes = DefaultMaxSizeMB * 1024 * 1024
	}
	if c.RotateInterval < 0 {
		c.RotateInterval = 0
	}
	if c.MaxBackups < 0 {
		c.MaxBackups = 0
	}
	if c.MaxAge < 0 {
		c.MaxAge = 0
	}
	return c
}

// ParseLevel 将配置中的字符串级别解析为 slog.Level，无法识别时回退为 INFO。
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// NewFileHandler 创建写入本地文件的 slog.Handler。
//
// 返回的 io.Closer 负责刷新并关闭文件句柄，应在进程退出前调用 Close。
func NewFileHandler(cfg Config) (slog.Handler, io.Closer, error) {
	w, err := newWriter(cfg)
	if err != nil {
		return nil, nil, err
	}

	opts := &slog.HandlerOptions{Level: cfg.Level}
	var handler slog.Handler
	if cfg.JSON {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return handler, w, nil
}
