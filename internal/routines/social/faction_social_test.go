// Package social 提供仙盟、圣盟、住宅与好友互动的帮派社交自动化玩法。
//
// @author Ateng
// @since 2026-11-02
package social

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestFactionSocialRoutines_MockExecute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_faction_social",
		RoleName:   "测试帮派角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("测试会话未进入激活状态: %s", session.State())
	}

	t.Run("帮派祭神循环上香", func(t *testing.T) {
		routine := NewFactionBlessingRoutine()
		if routine.Name() != "faction_blessing" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "social" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		if err := routine.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("帮派祭神执行失败: %v", err)
		}
		if got := session.LastActionID(); got != protocol.ActionIDFactionIncense {
			t.Errorf("期望最后动作为上香 0x%08X, 实际 0x%08X", protocol.ActionIDFactionIncense, got)
		}
	})

	t.Run("帮派转盘循环抽奖", func(t *testing.T) {
		routine := NewFactionLuckyWheelRoutine()
		if routine.Name() != "faction_lucky_wheel" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "social" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		if err := routine.Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("帮派转盘执行失败: %v", err)
		}
		if got := session.LastActionID(); got != protocol.ActionIDLuckyWheelStart {
			t.Errorf("期望最后动作为转盘抽奖 0x%08X, 实际 0x%08X", protocol.ActionIDLuckyWheelStart, got)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		if err := NewFactionBlessingRoutine().Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("帮派祭神空会话应返回 ErrNotConnected, 实际: %v", err)
		}
		if err := NewFactionLuckyWheelRoutine().Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("帮派转盘空会话应返回 ErrNotConnected, 实际: %v", err)
		}
	})
}

// TestFactionRoutines_SkipWhenUnavailable 验证已标记帮派模块不可用后，
// 帮派祭神与帮派转盘都不再发包，直接跳过本轮。
func TestFactionRoutines_SkipWhenUnavailable(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_faction_skip",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("测试会话未进入激活状态: %s", session.State())
	}

	sent := make(chan uint32, 16)
	session.SetPacketObserver(func(pkt *protocol.Packet) {
		if pkt == nil || pkt.ActionID == protocol.ActionHeartbeat {
			return
		}
		select {
		case sent <- pkt.ActionID:
		default:
		}
	})

	session.MarkFactionFeatureUnavailable()
	if err := NewFactionBlessingRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("帮派祭神跳过失败: %v", err)
	}
	if err := NewFactionLuckyWheelRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("帮派转盘跳过失败: %v", err)
	}

	select {
	case action := <-sent:
		t.Fatalf("帮派模块不可用时不应发包, 实际发送 0x%08X", action)
	default:
	}
}

// TestFactionBlessingRoutine_SilentDropMarksUnavailable 验证帮派只读请求静默超时后
// 会置位帮派不可用标记，使同一会话中的其余帮派玩法直接跳过。
func TestFactionBlessingRoutine_SilentDropMarksUnavailable(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			// 面板查询 (Act 22) 不回包；上香探测 (Act 23) 返回“无此帮派成员”，
			// 复刻线上未加入帮派角色的服务端行为。
			if pkt.ActionID == protocol.ActionIDFactionIncense {
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDFactionIncense,
					[]byte{protocol.FactionIncenseResultMemberNotExist},
				))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_faction_silent",
		ServerAddr: "mock-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *client.RoleSession) error {
			return nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("测试会话未进入激活状态: %s", session.State())
	}

	if err := NewFactionBlessingRoutine().Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("帮派祭神执行失败: %v", err)
	}
	if !session.IsFactionFeatureUnavailable() {
		t.Fatalf("静默超时后应标记帮派模块不可用")
	}
}
