// Package logging 内部单元测试，验证日志轮转、清理与级别过滤行为。
//
// @author Ateng
// @since 2026-11-03
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"unknown": slog.LevelInfo,
		"":        slog.LevelInfo,
	}
	for input, want := range cases {
		if got := ParseLevel(input); got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestWriter_RotateBySize(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(Config{
		Dir:            dir,
		FileName:       "sxd.log",
		MaxSizeBytes:   100,
		RotateInterval: 0,
	})
	if err != nil {
		t.Fatalf("newWriter failed: %v", err)
	}
	defer w.Close()

	first := strings.Repeat("a", 60) + "\n"
	second := strings.Repeat("b", 60) + "\n"
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if _, err := w.Write([]byte(second)); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	backups, _ := filepath.Glob(filepath.Join(dir, "sxd-*.log"))
	if len(backups) != 1 {
		t.Fatalf("expected 1 rotated file, got %d: %v", len(backups), backups)
	}
	backupData, _ := os.ReadFile(backups[0])
	if string(backupData) != first {
		t.Errorf("rotated file content = %q, want %q", backupData, first)
	}
	activeData, _ := os.ReadFile(filepath.Join(dir, "sxd.log"))
	if string(activeData) != second {
		t.Errorf("active file content = %q, want %q", activeData, second)
	}
}

func TestWriter_RotateByTime(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(Config{
		Dir:            dir,
		FileName:       "app.log",
		MaxSizeBytes:   1 << 20,
		RotateInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("newWriter failed: %v", err)
	}
	defer w.Close()

	clock := time.Now()
	w.now = func() time.Time { return clock }
	w.nextRotate = clock.Add(time.Hour)

	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	clock = clock.Add(2 * time.Hour)
	if _, err := w.Write([]byte("second\n")); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	backups, _ := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if len(backups) != 1 {
		t.Fatalf("expected 1 rotated file, got %d: %v", len(backups), backups)
	}
	backupData, _ := os.ReadFile(backups[0])
	if string(backupData) != "first\n" {
		t.Errorf("rotated file content = %q, want %q", backupData, "first\n")
	}
	activeData, _ := os.ReadFile(filepath.Join(dir, "app.log"))
	if string(activeData) != "second\n" {
		t.Errorf("active file content = %q, want %q", activeData, "second\n")
	}
}

func TestWriter_RotateOnStartup(t *testing.T) {
	dir := t.TempDir()
	activePath := filepath.Join(dir, "sxd.log")
	if err := os.WriteFile(activePath, []byte("previous-run\n"), 0o644); err != nil {
		t.Fatalf("seed active log failed: %v", err)
	}

	w, err := newWriter(Config{
		Dir:          dir,
		FileName:     "sxd.log",
		MaxSizeBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("newWriter failed: %v", err)
	}
	defer w.Close()

	backups, _ := filepath.Glob(filepath.Join(dir, "sxd-*.log"))
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup on startup, got %d: %v", len(backups), backups)
	}
	backupData, _ := os.ReadFile(backups[0])
	if string(backupData) != "previous-run\n" {
		t.Errorf("backup content = %q, want %q", backupData, "previous-run\n")
	}
	activeData, _ := os.ReadFile(activePath)
	if len(activeData) != 0 {
		t.Errorf("active log should be empty after startup rotation, got %q", activeData)
	}

	if _, err := w.Write([]byte("current-run\n")); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	activeData, _ = os.ReadFile(activePath)
	if string(activeData) != "current-run\n" {
		t.Errorf("active log content = %q, want %q", activeData, "current-run\n")
	}
}

func TestWriter_NoStartupRotationForEmptyFile(t *testing.T) {
	dir := t.TempDir()
	activePath := filepath.Join(dir, "sxd.log")
	if err := os.WriteFile(activePath, nil, 0o644); err != nil {
		t.Fatalf("seed empty log failed: %v", err)
	}

	w, err := newWriter(Config{Dir: dir, FileName: "sxd.log"})
	if err != nil {
		t.Fatalf("newWriter failed: %v", err)
	}
	defer w.Close()

	backups, _ := filepath.Glob(filepath.Join(dir, "sxd-*.log"))
	if len(backups) != 0 {
		t.Fatalf("empty active log should not be rotated on startup, got %v", backups)
	}
}

func TestWriter_PruneMaxBackups(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(Config{
		Dir:        dir,
		FileName:   "sxd.log",
		MaxBackups: 2,
	})
	if err != nil {
		t.Fatalf("newWriter failed: %v", err)
	}
	defer w.Close()

	base := time.Now().Add(-10 * time.Hour)
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("sxd-20260101-00000%d.log", i))
		if err := os.WriteFile(path, []byte("history"), 0o644); err != nil {
			t.Fatalf("write history failed: %v", err)
		}
		ts := base.Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("chtimes failed: %v", err)
		}
	}

	w.pruneLocked(time.Now())

	backups, _ := filepath.Glob(filepath.Join(dir, "sxd-*.log"))
	if len(backups) != 2 {
		t.Fatalf("expected 2 retained backups, got %d: %v", len(backups), backups)
	}
	for _, path := range backups {
		if strings.Contains(path, "000000") || strings.Contains(path, "000001") || strings.Contains(path, "000002") {
			t.Errorf("oldest backup should have been pruned: %s", path)
		}
	}
}

func TestWriter_PruneMaxAge(t *testing.T) {
	dir := t.TempDir()
	w, err := newWriter(Config{
		Dir:    dir,
		MaxAge: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("newWriter failed: %v", err)
	}
	defer w.Close()

	now := time.Now()
	oldPath := filepath.Join(dir, "sxd-20260101-000000.log")
	freshPath := filepath.Join(dir, "sxd-20260102-000000.log")
	for _, path := range []string{oldPath, freshPath} {
		if err := os.WriteFile(path, []byte("history"), 0o644); err != nil {
			t.Fatalf("write history failed: %v", err)
		}
	}
	oldTime := now.Add(-48 * time.Hour)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}
	freshTime := now.Add(-time.Hour)
	if err := os.Chtimes(freshPath, freshTime, freshTime); err != nil {
		t.Fatalf("chtimes failed: %v", err)
	}

	w.pruneLocked(now)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("expired log should have been removed: %s", oldPath)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Errorf("fresh log should be retained: %s", freshPath)
	}
}

func TestFileHandler_DefaultInfoLevel(t *testing.T) {
	dir := t.TempDir()
	// 零值 Config 的 Level 为 INFO，验证默认只保存 INFO 及以上日志。
	handler, closer, err := NewFileHandler(Config{
		Dir:          dir,
		FileName:     "app.log",
		MaxSizeBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("NewFileHandler failed: %v", err)
	}

	logger := slog.New(handler)
	logger.Debug("debug-message-should-be-dropped")
	logger.Info("info-message-should-be-saved")
	logger.Warn("warn-message-should-be-saved")
	if err := closer.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "debug-message-should-be-dropped") {
		t.Errorf("debug log should be filtered at default INFO level: %s", content)
	}
	if !strings.Contains(content, "info-message-should-be-saved") || !strings.Contains(content, "warn-message-should-be-saved") {
		t.Errorf("info/warn logs should be saved: %s", content)
	}
}

func TestFileHandler_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	handler, closer, err := NewFileHandler(Config{
		Dir:          dir,
		FileName:     "app.log",
		MaxSizeBytes: 1 << 20,
		JSON:         true,
	})
	if err != nil {
		t.Fatalf("NewFileHandler failed: %v", err)
	}
	slog.New(handler).Info("json-entry", "key", "value")
	if err := closer.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "app.log"))
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	if !strings.Contains(string(data), `"msg":"json-entry"`) || !strings.Contains(string(data), `"key":"value"`) {
		t.Errorf("expected JSON log entry, got: %s", data)
	}
}
