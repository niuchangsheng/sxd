// Package main 包含主程序单元测试用例。
//
// @author Ateng
// @since 2026-10-08
package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sxd-pie-ng/internal/web"
)

func TestRunVersion(t *testing.T) {
	err := run(context.Background(), []string{"-v"})
	if err != nil {
		t.Fatalf("run with -v failed: %v", err)
	}
}

func TestRun_InvalidConfig(t *testing.T) {
	err := run(context.Background(), []string{"-config", "non_existent_config.yaml"})
	if err == nil {
		t.Fatal("expected error with non-existent config file")
	}
}

func TestRun_FullBootstrapAndGracefulShutdown(t *testing.T) {
	// 获取一个动态分配的空闲端口，避免与常驻服务端口冲突
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen free port failed: %v", err)
	}
	freePort := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()

	testYaml := fmt.Sprintf(`
server:
  host: "127.0.0.1"
  port: %d

accounts:
  - platform: "test_plat"
    username: "user1"
    password: "pwd"
    roles:
      - server_id: "s1"
        role_name: "test_role"
        auto_login: false

scheduler:
  jitter:
    min_seconds: 0.1
    max_seconds: 0.2
`, freePort)
	tmpConfig := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(tmpConfig, []byte(testYaml), 0644); err != nil {
		t.Fatalf("write tmpConfig failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// 100ms 后触发优雅停机
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = run(ctx, []string{"-config", tmpConfig})
	if err != nil {
		t.Fatalf("run bootstrap failed: %v", err)
	}
}

func TestMultiHandler_DebugFilter(t *testing.T) {
	var buf bytes.Buffer
	stdoutHandler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logBuf := web.NewLogBuffer(10)

	multiH := &multiHandler{
		handlers: []slog.Handler{stdoutHandler, logBuf},
	}
	logger := slog.New(multiH)

	// 发送一条 Debug 日志，验证屏幕和 LogBuffer 均被过滤拦截
	logger.Debug("这条debug日志屏幕不应显示", "test_key", "debug_val")

	if buf.Len() > 0 {
		t.Fatalf("期望屏幕输出为空，实际输出了 Debug 日志: %s", buf.String())
	}
	if len(logBuf.GetLogs()) > 0 {
		t.Fatalf("期望LogBuffer中无Debug日志，实际条数: %d", len(logBuf.GetLogs()))
	}

	// 发送一条 Info 业务日志，验证正常输出
	logger.Info("这条info业务日志应该正常显示", "role_id", "test-role")
	if !strings.Contains(buf.String(), "这条info业务日志应该正常显示") {
		t.Fatalf("期望屏幕包含 Info 日志，实际输出: %s", buf.String())
	}
	if len(logBuf.GetLogs()) != 1 {
		t.Fatalf("期望LogBuffer记录1条Info日志，实际条数: %d", len(logBuf.GetLogs()))
	}
}
