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

// TestSelectStArenaOpponent 验证仙界竞技场软柿子算法：等级优先，其次积分，最后玩家 ID。
func TestSelectStArenaOpponent(t *testing.T) {
	if _, ok := SelectStArenaOpponent(nil); ok {
		t.Fatal("空对手列表应返回 false")
	}
	opponents := []protocol.StArenaOpponent{
		{StPlayerID: 30, Level: 80, Integral: 1000},
		{StPlayerID: 20, Level: 60, Integral: 1500},
		{StPlayerID: 10, Level: 60, Integral: 1200},
		{StPlayerID: 40, Level: 60, Integral: 1200}, // 与 10 同等级同积分，玩家 ID 更大
	}
	got, ok := SelectStArenaOpponent(opponents)
	if !ok {
		t.Fatal("期望选出对手")
	}
	if got.StPlayerID != 10 || got.Level != 60 || got.Integral != 1200 {
		t.Errorf("软柿子算法选择错误: %+v", got)
	}
}

// TestCelestialArenaRoutine_ConsumesAllChallenges 验证仙界竞技场会建立跨服连接，
// 循环消耗全部挑战次数，并在每场后重新打开面板确认状态。
func TestCelestialArenaRoutine_ConsumesAllChallenges(t *testing.T) {
	const (
		playerID        int32 = 65536
		initialTimes    int16 = 3
		mainServerAddr        = "main-st-arena"
		crossServerAddr       = "cross-st-arena:9000"
	)

	mainServer, mainClient := net.Pipe()
	crossServer, crossClient := net.Pipe()
	defer mainServer.Close()
	defer crossServer.Close()

	var challengeCalls atomic.Int32

	// ---- 主服：完成四步初始化并下发 Mod 96 跨服凭据 ----
	go func() {
		for {
			pkt, err := protocol.ReadPacket(mainServer)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerLogin, []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}))
			case protocol.ActionIDPlayerInitStep1:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerInitStep1,
					[]byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}))
			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))
			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))
			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x9a, 0x37,
					0x00, 0x00, 0x00, 0x08, 0x6f, 0x93, 0xac, 0xf8,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0a, 0xd1,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0f, 0x99,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionIDStcLoginGetInfo:
				w := protocol.NewWriter()
				w.WriteString("cross-st-arena")
				w.WriteUint16(9000)
				w.WriteString("s703")
				w.WriteInt32(1793549704)
				w.WriteString("mock_pass_code")
				w.WriteString("XianJie")
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDStcLoginGetInfo, w.Bytes()))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			}
		}
	}()

	// ---- 仙界跨服节点：Mod 94 登录 / Mod 95 进城 / Mod 214 竞技场 ----
	remain := initialTimes
	integral := int32(1000)
	reportID := int32(0)
	go func() {
		for {
			pkt, err := protocol.ReadPacket(crossServer)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDStLogin:
				w := protocol.NewWriter()
				w.WriteUint8(0)
				w.WriteInt32(playerID)
				w.WriteInt32(1793549704)
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStLogin, w.Bytes()))

			case protocol.ActionIDStTownEnter:
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStTownEnter, []byte{protocol.StTownResultSuccess}))

			case protocol.ActionIDStArenaOpen:
				w := protocol.NewWriter()
				w.WriteInt32(integral) // integral
				w.WriteInt16(remain)   // reduce_challenge_count
				w.WriteInt32(0)        // award_feats
				w.WriteInt32(0)        // award_get_time
				w.WriteInt32(1)        // reduce_refresh_time
				w.WriteInt32(0)        // proficiency
				w.WriteUint16(2)       // challengeList
				w.WriteInt32(7001)
				w.WriteInt32(1200)
				w.WriteInt32(101)
				w.WriteInt16(60)
				w.WriteInt32(7002)
				w.WriteInt32(1800)
				w.WriteInt32(102)
				w.WriteInt16(90)
				// reportList: 仅保存最近一场
				if reportID > 0 {
					w.WriteUint16(1)
					w.WriteInt32(reportID)
					w.WriteUint8(0)
					w.WriteInt32(playerID)
					w.WriteString("测试角色")
					w.WriteInt32(7001)
					w.WriteString("对手")
					w.WriteInt32(playerID)
					w.WriteInt32(25)
					w.WriteString("v1")
					w.WriteInt32(int32(time.Now().Unix()))
					w.WriteUint8(0)
				} else {
					w.WriteUint16(0)
				}
				w.WriteUint16(0) // integralRankList
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStArenaOpen, w.Bytes()))

			case protocol.ActionIDStArenaChallenge:
				req, pErr := protocol.ParseStArenaChallengeRequest(pkt.Payload)
				if pErr != nil {
					return
				}
				if req.StPlayerID != 7001 && req.StPlayerID != 7002 {
					t.Errorf("挑战目标不在对手列表: %+v", req)
					return
				}
				challengeCalls.Add(1)
				remain--
				integral += 25
				reportID = challengeCalls.Load()
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStArenaChallenge,
					[]byte{protocol.StArenaResultSuccess}))

			case protocol.ActionIDStArenaRefreshPlayerList:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.StArenaResultSuccess)
				w.WriteUint16(2)
				w.WriteInt32(7001)
				w.WriteInt32(1200)
				w.WriteInt32(101)
				w.WriteInt16(60)
				w.WriteInt32(7002)
				w.WriteInt32(1800)
				w.WriteInt32(102)
				w.WriteInt16(90)
				w.WriteInt32(1)
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStArenaRefreshPlayerList, w.Bytes()))

			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_st_arena",
		RoleName:   "仙界竞技场测试",
		ServerAddr: mainServerAddr,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == mainServerAddr {
				return mainClient, nil
			}
			if addr == crossServerAddr {
				return crossClient, nil
			}
			return nil, &net.AddrError{Err: "unexpected address", Addr: addr}
		},
	})
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.PlayerID = playerID
		ps.OriginNickName = "测试角色.s703"
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
		t.Fatalf("会话未进入激活态: %s", session.State())
	}

	execCtx, execCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer execCancel()
	if err := NewCelestialArenaRoutine().Execute(execCtx, session, nil); err != nil {
		t.Fatalf("仙界竞技场例程执行失败: %v", err)
	}
	if got := challengeCalls.Load(); got != int32(initialTimes) {
		t.Errorf("期望发起 %d 次挑战直至次数用尽, 实际 %d 次", initialTimes, got)
	}
	if remain != 0 {
		t.Errorf("期望挑战次数归零, 实际 %d", remain)
	}
	if integral != 1000+25*int32(initialTimes) {
		t.Errorf("期望积分按胜场累计, 实际 %d", integral)
	}

	session.Close()
	mainClient.Close()
	crossClient.Close()
}

// stArenaMockConfig 描述仙界竞技场跨服 mock 的挑战行为。
type stArenaMockConfig struct {
	// initialTimes 面板初始剩余挑战次数。
	initialTimes int16
	// challengeResult 挑战回包结果码。
	challengeResult uint8
	// replyChallenge 为 false 时模拟挑战回包超时 (服务端已结算但不下发回包)。
	replyChallenge bool
}

// startStArenaMock 搭建主服 + 仙界跨服双连接 mock 并返回已激活的主服会话。
func startStArenaMock(t *testing.T, cfg stArenaMockConfig) (*client.RoleSession, *atomic.Int32) {
	t.Helper()

	const (
		playerID        int32 = 65536
		mainServerAddr        = "main-st-arena-branch"
		crossServerAddr       = "cross-st-arena-branch:9000"
	)

	mainServer, mainClient := net.Pipe()
	crossServer, crossClient := net.Pipe()
	t.Cleanup(func() {
		mainClient.Close()
		crossClient.Close()
		mainServer.Close()
		crossServer.Close()
	})

	var challengeCalls atomic.Int32

	go func() {
		for {
			pkt, err := protocol.ReadPacket(mainServer)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDPlayerLogin:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerLogin, []byte{
					0x00, 0x00, 0x00, 0x00, 0x04, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x01, 0x00, 0x0b, 0x00, 0x00, 0x10, 0x85, 0x01,
				}))
			case protocol.ActionIDPlayerInitStep1:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerInitStep1,
					[]byte{0x00, 0x00, 0x13, 0x0f, 0x00, 0x02, 0x00, 0x00, 0x13, 0x0f, 0x00, 0x00, 0x13, 0x12}))
			case protocol.ActionIDPlayerInitStep2:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerInitStep2, []byte{0x00}))
			case protocol.ActionIDPlayerInitStep3:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerInitStep3, make([]byte, 8)))
			case protocol.ActionIDPlayerGetInfo:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDPlayerGetInfo, []byte{
					0x00, 0x09, 0xe6, 0xa2, 0xa6, 0xe4, 0xb8, 0x80, 0xe5, 0x9c, 0xba,
					0x00, 0x00, 0x01, 0x2c,
					0x00, 0x00, 0x9a, 0x37,
					0x00, 0x00, 0x00, 0x08, 0x6f, 0x93, 0xac, 0xf8,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0a, 0xd1,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x8c, 0x0f, 0x99,
					0x00, 0x00, 0x01, 0x2c,
				}))
			case protocol.ActionIDStcLoginGetInfo:
				w := protocol.NewWriter()
				w.WriteString("cross-st-arena-branch")
				w.WriteUint16(9000)
				w.WriteString("s703")
				w.WriteInt32(1793549704)
				w.WriteString("mock_pass_code")
				w.WriteString("XianJie")
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionIDStcLoginGetInfo, w.Bytes()))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(mainServer, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			}
		}
	}()

	remain := cfg.initialTimes
	integral := int32(1000)
	reportID := int32(0)
	go func() {
		for {
			pkt, err := protocol.ReadPacket(crossServer)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDStLogin:
				w := protocol.NewWriter()
				w.WriteUint8(0)
				w.WriteInt32(playerID)
				w.WriteInt32(1793549704)
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStLogin, w.Bytes()))
			case protocol.ActionIDStTownEnter:
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStTownEnter, []byte{protocol.StTownResultSuccess}))
			case protocol.ActionIDStArenaOpen:
				w := protocol.NewWriter()
				w.WriteInt32(integral)
				w.WriteInt16(remain)
				w.WriteInt32(0)
				w.WriteInt32(0)
				w.WriteInt32(1) // reduce_refresh_time
				w.WriteInt32(0) // proficiency
				w.WriteUint16(2)
				w.WriteInt32(7001)
				w.WriteInt32(1200)
				w.WriteInt32(101)
				w.WriteInt16(60)
				w.WriteInt32(7002)
				w.WriteInt32(1800)
				w.WriteInt32(102)
				w.WriteInt16(90)
				if reportID > 0 {
					w.WriteUint16(1)
					w.WriteInt32(reportID)
					w.WriteUint8(0)
					w.WriteInt32(playerID)
					w.WriteString("测试角色")
					w.WriteInt32(7001)
					w.WriteString("对手")
					w.WriteInt32(playerID)
					w.WriteInt32(25)
					w.WriteString("v1")
					w.WriteInt32(int32(time.Now().Unix()))
					w.WriteUint8(0)
				} else {
					w.WriteUint16(0)
				}
				w.WriteUint16(0)
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStArenaOpen, w.Bytes()))
			case protocol.ActionIDStArenaChallenge:
				n := challengeCalls.Add(1)
				// 成功或模拟超时均视为服务端已结算；明确失败码不扣次。
				if cfg.challengeResult == protocol.StArenaResultSuccess || !cfg.replyChallenge {
					remain--
					integral += 25
					reportID = n
				}
				if cfg.replyChallenge {
					_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStArenaChallenge,
						[]byte{cfg.challengeResult}))
				}
			case protocol.ActionIDStArenaRefreshPlayerList:
				w := protocol.NewWriter()
				w.WriteUint8(protocol.StArenaResultSuccess)
				w.WriteUint16(2)
				w.WriteInt32(7001)
				w.WriteInt32(1200)
				w.WriteInt32(101)
				w.WriteInt16(60)
				w.WriteInt32(7002)
				w.WriteInt32(1800)
				w.WriteInt32(102)
				w.WriteInt16(90)
				w.WriteInt32(1)
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionIDStArenaRefreshPlayerList, w.Bytes()))
			case protocol.ActionHeartbeat:
				_ = protocol.WritePacket(crossServer, protocol.NewPacket(protocol.ActionHeartbeat, []byte{0x06, 0x00, 0x0a, 0x28}))
			}
		}
	}()

	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     "role_st_arena_branch",
		RoleName:   "仙界竞技场分支测试",
		ServerAddr: mainServerAddr,
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if addr == mainServerAddr {
				return mainClient, nil
			}
			if addr == crossServerAddr {
				return crossClient, nil
			}
			return nil, &net.AddrError{Err: "unexpected address", Addr: addr}
		},
	})
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.PlayerID = playerID
		ps.OriginNickName = "测试角色.s703"
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	t.Cleanup(session.Close)

	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未进入激活态: %s", session.State())
	}
	return session, &challengeCalls
}

// TestCelestialArenaRoutine_ChallengeTimeoutReconciles 验证挑战回包超时后
// 会重新拉取面板核对次数：服务端已扣次则按已结算继续，不会中断整轮挑战。
func TestCelestialArenaRoutine_ChallengeTimeoutReconciles(t *testing.T) {
	oldTimeout := stArenaChallengeTimeout
	stArenaChallengeTimeout = 150 * time.Millisecond
	defer func() { stArenaChallengeTimeout = oldTimeout }()

	const initialTimes int16 = 3
	session, challengeCalls := startStArenaMock(t, stArenaMockConfig{
		initialTimes:    initialTimes,
		challengeResult: protocol.StArenaResultSuccess,
		replyChallenge:  false,
	})

	execCtx, execCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer execCancel()
	if err := NewCelestialArenaRoutine().Execute(execCtx, session, nil); err != nil {
		t.Fatalf("仙界竞技场例程执行失败: %v", err)
	}
	if got := challengeCalls.Load(); got != int32(initialTimes) {
		t.Errorf("回包超时后应继续消耗剩余次数, 期望 %d 次, 实际 %d 次", initialTimes, got)
	}
}

// TestCelestialArenaRoutine_NotInListReloadsPanel 验证结果码 18 (NOT_IN_LIST)
// 会被识别为目标失效并重拉面板，而非直接终止整轮挑战。
func TestCelestialArenaRoutine_NotInListReloadsPanel(t *testing.T) {
	session, challengeCalls := startStArenaMock(t, stArenaMockConfig{
		initialTimes:    5,
		challengeResult: protocol.StArenaResultNotInList,
		replyChallenge:  true,
	})

	execCtx, execCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer execCancel()
	if err := NewCelestialArenaRoutine().Execute(execCtx, session, nil); err != nil {
		t.Fatalf("仙界竞技场例程执行失败: %v", err)
	}
	want := int32(stArenaMaxStaleRetries + 1)
	if got := challengeCalls.Load(); got != want {
		t.Errorf("NOT_IN_LIST 应按最大重试次数重拉面板后退出, 期望 %d 次挑战, 实际 %d 次", want, got)
	}
}
