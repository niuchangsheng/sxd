// Package main_test 验证诊断探针命令行工具的参数解析、真实握手闭环与资产嗅探指标。
//
// @author Ateng
// @since 2026-10-10
package main

import (
	"bytes"
	"context"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestResolveProbeConfig 验证命令行参数与配置文件的继承覆盖逻辑
func TestResolveProbeConfig(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test_probe_config_*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp config: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	mockYAML := `accounts:
  - platform: "fengwan"
    username: "kongyu"
    password: "pwd"
    roles:
      - server_id: "s813"
        role_name: "梦一场"
`
	if err := os.WriteFile(tmpFile.Name(), []byte(mockYAML), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	opts := ProbeOptions{
		ConfigPath: tmpFile.Name(),
		ServerAddr: "custom-gateway:8381",
		RoleName:   "自定义角色",
	}

	res := resolveProbeConfig(opts)

	if res.ServerAddr != "custom-gateway:8381" {
		t.Errorf("期望命令行 ServerAddr 覆盖配置, 实际为 %s", res.ServerAddr)
	}
	if res.RoleName != "自定义角色" {
		t.Errorf("期望命令行 RoleName 覆盖配置, 实际为 %s", res.RoleName)
	}
	// 验证未覆盖字段继承自配置文件
	if res.Platform != "fengwan" {
		t.Errorf("期望 Platform 继承自配置文件 'fengwan', 实际为 %s", res.Platform)
	}
	if res.Username != "kongyu" {
		t.Errorf("期望 Username 继承自配置文件 'kongyu', 实际为 %s", res.Username)
	}
}

// TestRunProbe_DryRun_FullLifecycle 验证沙箱模式下五阶段握手、城镇场景同步、资产快照与心跳往返的全生命周期闭环
func TestRunProbe_DryRun_FullLifecycle(t *testing.T) {
	var out bytes.Buffer

	opts := ProbeOptions{
		ConfigPath: "configs/test_non_existent.yaml",
		RoleName:   "梦一场",
		DryRun:     true,
		OneShot:    true,
		Timeout:    3 * time.Second,
		Output:     &out,
	}

	report, err := RunProbe(context.Background(), opts)
	if err != nil {
		t.Fatalf("RunProbe 失败: %v, 输出: %s", err, out.String())
	}

	if !report.Success {
		t.Fatal("期望报告标记为 Success")
	}

	// 1. 验证 ResultCode
	if report.ResultCode != 4 {
		t.Errorf("期望 ResultCode 为 4, 实际为 %d", report.ResultCode)
	}

	// 2. 验证场景编号 TownID
	if report.TownID != 4879 {
		t.Errorf("期望 TownID 为 4879, 实际为 %d", report.TownID)
	}

	// 3. 验证角色资产快照
	if report.RoleName != "梦一场" {
		t.Errorf("期望角色名为 '梦一场', 实际为 '%s'", report.RoleName)
	}
	if report.Level != 300 {
		t.Errorf("期望角色等级 300, 实际为 %d", report.Level)
	}
	if report.Coins != 36231687416 {
		t.Errorf("期望铜钱 36231687416, 实际为 %d", report.Coins)
	}
	if report.Ingots != 39479 {
		t.Errorf("期望元宝 39479, 实际为 %d", report.Ingots)
	}
	if report.Stamina != 201 && report.Stamina != 300 {
		t.Errorf("期望体力在合理区间 (201/300), 实际为 %d", report.Stamina)
	}

	// 4. 验证心跳交互已记录
	if report.HeartbeatRTT <= 0 {
		t.Errorf("期望心跳 RTT 大于 0, 实际为 %v", report.HeartbeatRTT)
	}
	if report.HeartbeatCount < 1 {
		t.Errorf("期望至少完成 1 次心跳交互, 实际为 %d", report.HeartbeatCount)
	}

	// 5. 校验控制台格式化诊断面板输出
	outStr := out.String()
	expectedSubstrings := []string{
		"[神仙道诊断探针] 启动真实端到端链路探测",
		"握手成功",
		"心跳交互成功",
		"网关链路与角色全量资产诊断面板",
		"角色等级   : 300 级",
		"当前城镇ID : 4879",
	}
	for _, sub := range expectedSubstrings {
		if !strings.Contains(outStr, sub) {
			t.Errorf("控制台输出缺少关键诊断信息: '%s'", sub)
		}
	}
}

// TestRunProbe_Rejected 验证当服务端返回凭证校验失败时给出明确异常并安全退出
func TestRunProbe_Rejected(t *testing.T) {
	var out bytes.Buffer

	// 自定义拒绝连接的 mock dialer
	rejectDialer := func(ctx context.Context, network, addr string) (net.Conn, error) {
		c1, c2 := net.Pipe()
		go func() {
			defer c2.Close()
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			if pkt.ActionID == protocol.ActionIDPlayerLogin {
				// 返回 ResultCode=1 凭据错误
				failPayload := []byte{0x00, 0x00, 0x00, 0x00, 0x01}
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerLogin, failPayload))
			}
		}()
		return c1, nil
	}

	opts := ProbeOptions{
		ServerAddr: "reject-gateway:8381",
		RoleName:   "非法角色",
		Username:   "bad_user",
		Hash:       "bad_hash",
		Timeout:    1500 * time.Millisecond,
		Output:     &out,
		Dialer:     rejectDialer,
	}

	report, err := RunProbe(context.Background(), opts)
	if err == nil {
		t.Fatal("期望握手被服务端拒绝时返回错误，但得到了 nil")
	}

	if report.Success {
		t.Fatal("期望 report.Success 为 false")
	}
}
