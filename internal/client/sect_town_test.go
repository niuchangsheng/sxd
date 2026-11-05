// Package client_test 验证仙盟城镇会话的凭据探测超时行为。
package client_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
)

// TestFetchSectTownCredentials_ShortTimeoutOnSilentServer 验证未入盟角色遇到
// 服务端静默无回包时，主服查询按 SectQueryTimeout 快速失败，而不是拖满调用方的长超时。
func TestFetchSectTownCredentials_ShortTimeoutOnSilentServer(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// 服务端只收不回，模拟未入盟角色查询仙盟面板时的静默丢弃。
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := serverConn.Read(buf); err != nil {
				return
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:   "role_test_sect_timeout",
		RoleName: "测试仙盟查询超时角色",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		SectQueryTimeout: 100 * time.Millisecond,
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	_, err := session.FetchSectTownCredentials(context.Background())
	if err == nil {
		t.Fatal("期望查询仙盟面板超时")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("期望 DeadlineExceeded, 实际: %v", err)
	}
	if !strings.Contains(err.Error(), "无回包") {
		t.Fatalf("错误信息应说明无回包语义, 实际: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("主服查询未使用短超时, 耗时 %v", elapsed)
	}
}
