// Package pvp 提供本服竞技场、仙界竞技场、神魔大战与劫镖等对抗性玩法。
//
// @author Ateng
// @since 2026-10-11
package pvp

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func TestArenaRoutine_Execute(t *testing.T) {
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_test_pvp",
		RoleName:   "测试竞技场角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	defer session.Close()

	time.Sleep(100 * time.Millisecond)

	t.Run("竞技场挑战全链路", func(t *testing.T) {
		routine := NewArenaRoutine()
		if routine.Name() != "arena" {
			t.Fatalf("任务名称不匹配: %s", routine.Name())
		}
		if dr, ok := routine.(scheduler.DetailedRoutine); ok {
			if dr.Domain() != "pvp" {
				t.Fatalf("领域不匹配: %s", dr.Domain())
			}
		}

		err := routine.Execute(context.Background(), session, nil)
		if err != nil {
			t.Fatalf("竞技场例程执行失败: %v", err)
		}
	})

	t.Run("软柿子筛选算法单测", func(t *testing.T) {
		opponents := []protocol.ArenaOpponent{
			{Rank: 10, RoleID: 101, Name: "强力剑宗", Level: 60},
			{Rank: 8, RoleID: 102, Name: "挂机小号", Level: 20},
			{Rank: 7, RoleID: 103, Name: "平民飞羽", Level: 30},
			{Rank: 6, RoleID: 104, Name: "老玩家", Level: 20}, // 等级同为 20，但 Rank 8 排位更低、更弱
		}

		target := SelectSoftPersimmonOpponent(opponents)
		if target.Rank != 8 || target.Level != 20 {
			t.Errorf("软柿子算法未能精准选出等级最低且排位最低者: %+v", target)
		}
	})

	t.Run("空会话防御", func(t *testing.T) {
		routine := NewArenaRoutine()
		if err := routine.Execute(context.Background(), nil, nil); err != client.ErrNotConnected {
			t.Errorf("期望 ErrNotConnected, 实际: %v", err)
		}
	})
}

// TestArenaRoutine_ConsumesAllChallengeTimes 验证单次调度会循环挑战直至次数用尽。
func TestArenaRoutine_ConsumesAllChallengeTimes(t *testing.T) {
	const initialTimes int16 = 3

	c1, c2 := net.Pipe()
	var challengeCalls atomic.Int32
	remain := initialTimes

	go func() {
		for {
			pkt, err := protocol.ReadPacket(c2)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerLogin, []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}))
			case protocol.ActionIDPlayerInitStep1:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep1,
					[]byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}))
			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))
			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))
			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x9a, 0x37,
					0x00, 0x00, 0x00, 0x08, 0x6f, 0x93, 0xac, 0xf8,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0a, 0xd1,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0f, 0x99,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(c2, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))

			case protocol.ActionIDArenaGetTimes:
				resp, _ := protocol.BuildArenaGetTimesResultPacket(int32(remain))
				_ = protocol.WritePacket(c2, resp)

			case protocol.ActionIDArenaOpenPanel:
				resp, _ := protocol.BuildArenaPanelResultPacket(protocol.ArenaPanelResult{
					MyRank:               100,
					RemainChallengeTimes: remain,
					Opponents: []protocol.ArenaOpponent{
						{Rank: 90, PlayerID: 1001, RoleID: 1, Name: "低级对手", Level: 10},
						{Rank: 80, PlayerID: 1002, RoleID: 2, Name: "高级对手", Level: 40},
					},
				})
				_ = protocol.WritePacket(c2, resp)

			case protocol.ActionIDArenaChallenge:
				n := challengeCalls.Add(1)
				remain--
				// 首场模拟战败: 服务端下发 2 秒冷却 (Unix 绝对时间戳)
				var winPlayerID, cdTimer int32
				if n == 1 {
					winPlayerID = -1
					cdTimer = int32(time.Now().Unix() + 2)
				}
				resp, _ := protocol.BuildArenaChallengeResultPacket(protocol.ArenaChallengeResult{
					Result:      protocol.ArenaResultSuccess,
					Success:     true,
					RemainTimes: remain,
					CdTimer:     cdTimer,
					ReportID:    int32(n),
					DeployType:  3,
					WinPlayerID: winPlayerID,
				})
				_ = protocol.WritePacket(c2, resp)
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_arena_loop",
		RoleName:   "竞技场循环测试",
		ServerAddr: "arena-loop-pipe",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return c1, nil
		},
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}

	execCtx, execCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer execCancel()
	start := time.Now()
	if err := NewArenaRoutine().Execute(execCtx, session, nil); err != nil {
		t.Fatalf("竞技场例程执行失败: %v", err)
	}
	elapsed := time.Since(start)

	if got := challengeCalls.Load(); got != int32(initialTimes) {
		t.Errorf("期望循环发起 %d 次挑战直至次数用尽, 实际发起 %d 次", initialTimes, got)
	}
	// 首场战败下发 2 秒冷却，必须等待后才发起第二场
	if elapsed < time.Second {
		t.Errorf("期望战败后等待服务端冷却再继续, 实际耗时仅 %v", elapsed)
	}

	session.Close()
	c1.Close()
	c2.Close()
}

// TestArenaCooldownDuration 验证 cdTimer 归一化: 剩余秒数与 Unix 绝对时间戳两种形态。
func TestArenaCooldownDuration(t *testing.T) {
	if d := arenaCooldownDuration(0); d != 0 {
		t.Errorf("cdTimer=0 应无冷却, 实际 %v", d)
	}
	if d := arenaCooldownDuration(-5); d != 0 {
		t.Errorf("cdTimer<0 应无冷却, 实际 %v", d)
	}
	if d := arenaCooldownDuration(30); d != 30*time.Second {
		t.Errorf("剩余秒数应原样换算, 实际 %v", d)
	}

	expired := int32(time.Now().Add(-10 * time.Second).Unix())
	if d := arenaCooldownDuration(expired); d != 0 {
		t.Errorf("已过期的绝对时间戳应无冷却, 实际 %v", d)
	}

	upcoming := int32(time.Now().Add(10 * time.Second).Unix())
	if d := arenaCooldownDuration(upcoming); d < 8*time.Second || d > 10*time.Second {
		t.Errorf("绝对时间戳应换算为剩余时长, 实际 %v", d)
	}

	if d := arenaCooldownDuration(3600); d != arenaCooldownMaxWait {
		t.Errorf("超长冷却应截断为 %v, 实际 %v", arenaCooldownMaxWait, d)
	}
}
