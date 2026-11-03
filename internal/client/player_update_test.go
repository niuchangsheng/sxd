// Package client_test 验证 2026 网关角色属性变更推送 (Module 0, Action 3) 的解析与状态同步。
//
// @author Ateng
// @since 2026-11-01
package client_test

import (
	"context"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// TestRoleSession_PlayerUpdateData_ModernBatch 验证批量属性包不会污染等级/体力,
// 且经验与铜钱能被正确同步。
func TestRoleSession_PlayerUpdateData_ModernBatch(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-player-update",
		RoleName:   "属性同步角色",
		ServerAddr: "mock",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("期望会话激活, 实际 %s", session.State())
	}

	before := session.GetPlayerState()

	// 真实抓包样本: 经验 303456 + 铜钱 50435193 (完成任务 251 之后)。
	payload, err := hex.DecodeString("000219000000000004a160140000000003019479")
	if err != nil {
		t.Fatalf("解码实测样本失败: %v", err)
	}
	if err := protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerUpdateData, payload)); err != nil {
		t.Fatalf("写入属性包失败: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	updated := session.GetPlayerState()
	for updated.Experience != 303456 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		updated = session.GetPlayerState()
	}

	if updated.Experience != 303456 {
		t.Errorf("经验未同步: 实际 %d", updated.Experience)
	}
	if updated.Coins != 50435193 {
		t.Errorf("铜钱未同步: 实际 %d", updated.Coins)
	}
	if updated.Level != before.Level {
		t.Errorf("等级被异常改写: 之前 %d, 之后 %d", before.Level, updated.Level)
	}
	if updated.Stamina != before.Stamina {
		t.Errorf("体力被异常改写: 之前 %d, 之后 %d", before.Stamina, updated.Stamina)
	}
}

// TestRoleSession_PlayerUpdateData_OnlineGiftState 验证在线礼包状态属性 81/82 的同步:
// prop 81 为剩余秒数 (换算为绝对时间戳), prop 82 为 -1/0/1 状态。
func TestRoleSession_PlayerUpdateData_OnlineGiftState(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-online-gift-state",
		RoleName:   "在线礼包状态角色",
		ServerAddr: "mock",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("期望会话激活, 实际 %s", session.State())
	}

	if got := session.GetPlayerState().IsGetOnlineGift; got != -2 {
		t.Fatalf("初始 IsGetOnlineGift 期望 -2, 实际 %d", got)
	}

	// 2026 网关批量布局: count=2, prop81=300 (剩余秒数), prop82=1 (可领取)
	payload, err := hex.DecodeString("000251000000000000012c520000000000000001")
	if err != nil {
		t.Fatalf("解码在线礼包属性样本失败: %v", err)
	}
	before := time.Now().Unix()
	if err := protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerUpdateData, payload)); err != nil {
		t.Fatalf("写入属性包失败: %v", err)
	}

	deadline = time.Now().Add(2 * time.Second)
	updated := session.GetPlayerState()
	for updated.IsGetOnlineGift != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		updated = session.GetPlayerState()
	}

	if updated.IsGetOnlineGift != 1 {
		t.Errorf("IsGetOnlineGift 期望 1, 实际 %d", updated.IsGetOnlineGift)
	}
	if updated.OnlineGiftTime < before+295 || updated.OnlineGiftTime > before+305 {
		t.Errorf("OnlineGiftTime 期望约 %d, 实际 %d", before+300, updated.OnlineGiftTime)
	}
}

// TestRoleSession_PlayerUpdateData_ClampsGarbage 验证旧版布局中的越界数值不会污染等级与体力。
func TestRoleSession_PlayerUpdateData_ClampsGarbage(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role-player-update-clamp",
		RoleName:   "属性校验角色",
		ServerAddr: "mock",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("期望会话激活, 实际 %s", session.State())
	}

	before := session.GetPlayerState()

	// 旧版布局中属性 15 携带 0x23000000 (587202560) 的错位脏值, 必须被丢弃。
	payload, err := hex.DecodeString("0f23000000")
	if err != nil {
		t.Fatalf("解码脏值样本失败: %v", err)
	}
	if err := protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerUpdateData, payload)); err != nil {
		t.Fatalf("写入属性包失败: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	updated := session.GetPlayerState()
	if updated.Stamina != before.Stamina {
		t.Errorf("越界体力脏值未被过滤: 之前 %d, 之后 %d", before.Stamina, updated.Stamina)
	}
	if updated.Level != before.Level {
		t.Errorf("等级被异常改写: 之前 %d, 之后 %d", before.Level, updated.Level)
	}
}
