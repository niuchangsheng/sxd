package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

func TestRoleSession_BeelzebubTrials(t *testing.T) {
	// 启动基于 runMockGameServer 的模拟测试会话
	cfg := SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	}
	s := NewRoleSession(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// 1. 测试今日挑战检查
	canChallenge, err := s.BeelzebubTrialsCheckCanChallenge(ctx)
	if err != nil {
		t.Fatalf("BeelzebubTrialsCheckCanChallenge failed: %v", err)
	}
	if !canChallenge {
		t.Fatalf("expected canChallenge true, got false")
	}

	// 2. 测试获取可挑战魔王列表
	canList, err := s.BeelzebubTrialsGetCanChallengeList(ctx)
	if err != nil {
		t.Fatalf("BeelzebubTrialsGetCanChallengeList failed: %v", err)
	}
	if len(canList) != 3 || canList[2] != 72 {
		t.Fatalf("expected 3 bosses with boss 72 (魔掌门神识), got %v", canList)
	}

	// 3. 测试打开主面板
	openData, err := s.BeelzebubTrialsOpen(ctx)
	if err != nil {
		t.Fatalf("BeelzebubTrialsOpen failed: %v", err)
	}
	if openData.MoralExp != 5000 || openData.MoralLv != 3 || !openData.MoralCanGet || !openData.InActivity {
		t.Fatalf("unexpected openData: %+v", openData)
	}

	// 4. 测试自建队伍
	cRes, err := s.BeelzebubTrialsCreateTeam(ctx, 72, protocol.BeelzebubTrialsNotLimit)
	if err != nil || cRes != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("BeelzebubTrialsCreateTeam failed: %v, res: %d", err, cRes)
	}

	// 5. 测试获取队伍信息
	leaderID, members, err := s.BeelzebubTrialsGetPlayerTeamInfo(ctx)
	if err != nil || leaderID != 1001 || len(members) != 1 {
		t.Fatalf("BeelzebubTrialsGetPlayerTeamInfo failed: %v, leader: %d, members: %v", err, leaderID, members)
	}

	// 6. 测试获取助战伙伴列表
	helpers, err := s.BeelzebubTrialsGetHelperRoles(ctx)
	if err != nil || len(helpers) != 2 {
		t.Fatalf("BeelzebubTrialsGetHelperRoles failed: %v, helpers: %v", err, helpers)
	}

	// 7. 测试挑选助战伙伴
	chRes, err := s.BeelzebubTrialsChooseHelper(ctx, helpers[0].RoleID, 1)
	if err != nil || chRes != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("BeelzebubTrialsChooseHelper failed: %v, res: %d", err, chRes)
	}

	// 8. 测试发起战斗
	fRes, err := s.BeelzebubTrialsFight(ctx)
	if err != nil || fRes != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("BeelzebubTrialsFight failed: %v, res: %d", err, fRes)
	}

	// 9. 测试拉取结算战报
	warRes, err := s.BeelzebubTrialsGetWarResult(ctx)
	if err != nil || warRes.Result != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("BeelzebubTrialsGetWarResult failed: %v, warRes: %+v", err, warRes)
	}

	// 10. 测试离开战场与退队
	if err := s.BeelzebubTrialsLeaveWar(ctx, 0); err != nil {
		t.Fatalf("BeelzebubTrialsLeaveWar failed: %v", err)
	}
	if err := s.BeelzebubTrialsExitTeam(ctx); err != nil {
		t.Fatalf("BeelzebubTrialsExitTeam failed: %v", err)
	}

	// 11. 测试道行查询与领取
	moral, err := s.BeelzebubTrialsOpenMoral(ctx)
	if err != nil || moral.Level != 2 || !moral.CanClaim {
		t.Fatalf("BeelzebubTrialsOpenMoral failed: %v, moral: %+v", err, moral)
	}
	mRes, err := s.BeelzebubTrialsGetMoralAward(ctx, moral.Level)
	if err != nil || mRes != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("BeelzebubTrialsGetMoralAward failed: %v, res: %d", err, mRes)
	}
}

func TestRoleSession_BeelzebubTrialsAutoChallenge(t *testing.T) {
	// 启动基于 runMockGameServer 的模拟测试会话
	cfg := SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	}
	s := NewRoleSession(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// 执行完整自动挑战闭环 (测试全自动从低等级优先锁定挑战)
	resDefault, err := s.BeelzebubTrialsAutoChallenge(ctx, 0)
	if err != nil {
		t.Fatalf("BeelzebubTrialsAutoChallenge (default 0) failed: %v", err)
	}
	if resDefault == nil || resDefault.Result != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("expected successful challenge, got %+v", resDefault)
	}

	// 执行完整自动挑战闭环 (优先指定 Boss 72 魔掌门神识)
	res, err := s.BeelzebubTrialsAutoChallenge(ctx, 72)
	if err != nil {
		t.Fatalf("BeelzebubTrialsAutoChallenge failed: %v", err)
	}
	if res == nil || res.Result != protocol.BeelzebubTrialsSuccess {
		t.Fatalf("expected successful challenge, got %+v", res)
	}
}

// TestRoleSession_BeelzebubTrialsAutoChallengeAll 验证逐关挑战会遍历可挑战列表中的全部关卡，
// 并在清完初始关卡后刷新列表，继续挑战新解锁的隐藏/神秘层关卡。
func TestRoleSession_BeelzebubTrialsAutoChallengeAll(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var mu sync.Mutex
	createdBosses := make([]int32, 0, 5)
	fightCount := 0

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCheckCanChallenge,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo:
				// 无遗留队伍，允许 AutoChallengeAll 继续建队。
				tw := protocol.NewWriter()
				tw.WriteInt32(0)
				tw.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, tw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsOpen:
				// 不关卡卡状态（与 runMockGameServer 一致），跳过可用性预检。
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpen, beelzebubOpenPayload(nil),
				))
			case protocol.ActionIDBeelzebubTrialsGetCanChallengeList:
				// 初始只返回 3 个普通关卡；清完后服务端解锁两个神秘层关卡：
				// 41 = 魔剑 (30 级)，42 = 紫霞仙子 (60 级)。
				mu.Lock()
				completed := len(createdBosses)
				mu.Unlock()

				bw := protocol.NewWriter()
				count := uint16(3)
				if completed >= 3 {
					count = 5
				}
				bw.WriteUint16(count)
				bw.WriteInt32(1)  // 魔化大魔王 (100 级)
				bw.WriteInt32(71) // 魔掌门元识 (30 级)
				bw.WriteInt32(72) // 魔掌门神识 (30 级)
				if completed >= 3 {
					bw.WriteInt32(41) // 神秘层·魔剑 (30 级)
					bw.WriteInt32(42) // 神秘层·紫霞仙子 (60 级)
				}
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetCanChallengeList, bw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsCreateTeam:
				r := protocol.NewReader(pkt.Payload)
				bossID, _ := r.ReadInt32()
				mu.Lock()
				createdBosses = append(createdBosses, bossID)
				mu.Unlock()
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCreateTeam,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsGetHelperRoleList:
				hw := protocol.NewWriter()
				hw.WriteUint16(0) // 无可用助战，直接发起战斗
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetHelperRoleList, hw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsFight:
				mu.Lock()
				fightCount++
				mu.Unlock()
				// Action 13 为单向通知，不回包。
			case protocol.ActionIDBeelzebubTrialsGetWarResult:
				ww := protocol.NewWriter()
				ww.WriteUint8(protocol.BeelzebubTrialsSuccess)
				ww.WriteInt32(0)
				ww.WriteInt32(0)
				ww.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetWarResult, ww.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsLeaveWar:
				// 单向离场通知，不回包。
			case protocol.ActionIDBeelzebubTrialsExitTeam:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsExitTeam,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsOpenMoral:
				mw := protocol.NewWriter()
				mw.WriteInt32(0) // exp
				mw.WriteInt32(0) // lv
				mw.WriteInt32(0) // 不可领取
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpenMoral, mw.Bytes(),
				))
			}
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "mock-pipe",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	results, err := s.BeelzebubTrialsAutoChallengeAll(ctx, 0)
	if err != nil {
		t.Fatalf("BeelzebubTrialsAutoChallengeAll failed: %v", err)
	}

	// 初始 3 关 + 刷新后新解锁的 2 个神秘层关卡，共 5 关都应完成。
	if len(results) != 5 {
		t.Fatalf("expected 5 challenge results (all stages incl. hidden), got %d", len(results))
	}
	for i, res := range results {
		if res == nil || res.Result != protocol.BeelzebubTrialsSuccess {
			t.Fatalf("result #%d expected success, got %+v", i, res)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if fightCount != 5 {
		t.Fatalf("expected 5 fights (one per stage), got %d", fightCount)
	}
	wantBosses := []int32{71, 72, 1, 41, 42}
	if len(createdBosses) != len(wantBosses) {
		t.Fatalf("expected create_team for %v, got %v", wantBosses, createdBosses)
	}
	for i, want := range wantBosses {
		if createdBosses[i] != want {
			t.Fatalf("expected create_team order %v, got %v", wantBosses, createdBosses)
		}
	}
}

// TestRoleSession_BeelzebubTrialsAutoChallengeAll_SilentDropCircuitBreaker 验证建队请求被服务端
// 静默丢弃（超时且无任何回包）时，逐关挑战会在连续两次无回包后熔断本轮，
// 而不是对可挑战列表中的每个关卡反复重试。
func TestRoleSession_BeelzebubTrialsAutoChallengeAll_SilentDropCircuitBreaker(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var mu sync.Mutex
	createAttempts := 0

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCheckCanChallenge,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo:
				// 无遗留队伍，允许 AutoChallengeAll 继续建队。
				tw := protocol.NewWriter()
				tw.WriteInt32(0)
				tw.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, tw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsOpen:
				// 不关卡卡状态（与 runMockGameServer 一致），跳过可用性预检。
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpen, beelzebubOpenPayload(nil),
				))
			case protocol.ActionIDBeelzebubTrialsGetCanChallengeList:
				bw := protocol.NewWriter()
				bw.WriteUint16(3)
				bw.WriteInt32(71)
				bw.WriteInt32(72)
				bw.WriteInt32(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetCanChallengeList, bw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsCreateTeam:
				mu.Lock()
				createAttempts++
				mu.Unlock()
				// 关键：不回包，模拟服务端静默丢弃建队动作。
			}
		}
	}()

	s := NewRoleSession(SessionConfig{
		// 复用 sandbox 的短超时（300ms），测试无需等待真实 3 秒。
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	results, err := s.BeelzebubTrialsAutoChallengeAll(ctx, 0)
	if !errors.Is(err, ErrBeelzebubTrialsNoResponse) {
		t.Fatalf("expected ErrBeelzebubTrialsNoResponse, got %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no challenge results after circuit break, got %d", len(results))
	}

	mu.Lock()
	defer mu.Unlock()
	if createAttempts != beelzebubSilentDropThreshold {
		t.Fatalf("expected circuit break after %d create attempts, got %d",
			beelzebubSilentDropThreshold, createAttempts)
	}
}

// TestRoleSession_BeelzebubTrialsCreateTeam_WrapsSilentDrop 验证建队请求超时时会被识别为
// 服务端静默丢弃，方便上层按确定性拒绝处理，而不是当成普通业务失败码。
func TestRoleSession_BeelzebubTrialsCreateTeam_WrapsSilentDrop(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go func() {
		for {
			if _, err := protocol.ReadPacket(serverConn); err != nil {
				return
			}
			// 不回复任何报文。
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	_, err := s.BeelzebubTrialsCreateTeam(ctx, 71, protocol.BeelzebubTrialsNotLimit)
	if !errors.Is(err, ErrBeelzebubTrialsNoResponse) {
		t.Fatalf("expected ErrBeelzebubTrialsNoResponse, got %v", err)
	}
}

// beelzebubOpenPayload 构造含关卡状态列表的面板回包，用于可用性预检测试。
func beelzebubOpenPayload(bosses []protocol.BeelzebubBossStatus) []byte {
	w := protocol.NewWriter()
	w.WriteUint16(0) // 无现有队伍
	w.WriteInt32(0)  // 附加 Int32
	w.WriteUint16(uint16(len(bosses)))
	for _, b := range bosses {
		w.WriteInt32(b.BossID)
		w.WriteInt32(b.Times)
		if b.TodayChallenged {
			w.WriteInt32(1)
		} else {
			w.WriteInt32(0)
		}
	}
	w.WriteInt32(0) // moralExp
	w.WriteInt32(1) // moralLv
	w.WriteInt32(0) // bGet
	w.WriteUint8(protocol.BeelzebubTrialsOnActivity)
	return w.Bytes()
}

// TestRoleSession_BeelzebubTrialsAutoChallengeAll_AllBossesCleared 验证面板显示所有关卡
// 均已完成（今日已挑战或累计次数达上限）时，自动化不再发送任何 create_team 请求。
func TestRoleSession_BeelzebubTrialsAutoChallengeAll_AllBossesCleared(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var mu sync.Mutex
	createAttempts := 0

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCheckCanChallenge,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo:
				// 无遗留队伍，允许 AutoChallengeAll 继续建队。
				tw := protocol.NewWriter()
				tw.WriteInt32(0)
				tw.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, tw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsOpen:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpen,
					beelzebubOpenPayload([]protocol.BeelzebubBossStatus{
						{BossID: 71, Times: 3},                        // 累计次数达上限
						{BossID: 72, Times: 0, TodayChallenged: true}, // 今日已挑战
						{BossID: 1, Times: 7},                         // 累计次数达上限
					}),
				))
			case protocol.ActionIDBeelzebubTrialsGetCanChallengeList:
				bw := protocol.NewWriter()
				bw.WriteUint16(3)
				bw.WriteInt32(71)
				bw.WriteInt32(72)
				bw.WriteInt32(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetCanChallengeList, bw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsCreateTeam:
				mu.Lock()
				createAttempts++
				mu.Unlock()
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCreateTeam,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsOpenMoral:
				mw := protocol.NewWriter()
				mw.WriteInt32(0)
				mw.WriteInt32(0)
				mw.WriteInt32(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpenMoral, mw.Bytes(),
				))
			}
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	results, err := s.BeelzebubTrialsAutoChallengeAll(ctx, 0)
	if err != nil {
		t.Fatalf("expected clean skip, got error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no results when all bosses cleared, got %d", len(results))
	}
	mu.Lock()
	defer mu.Unlock()
	if createAttempts != 0 {
		t.Fatalf("expected no create_team requests when all bosses cleared, got %d", createAttempts)
	}
}

// TestRoleSession_BeelzebubTrialsAutoChallengeAll_FiltersClearedBosses 验证面板中
// 已完成的关卡会被跳过，而仍有剩余次数与面板未收录的新解锁关卡仍然挑战。
func TestRoleSession_BeelzebubTrialsAutoChallengeAll_FiltersClearedBosses(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	var mu sync.Mutex
	createdBosses := make([]int32, 0, 3)

	go func() {
		for {
			pkt, err := protocol.ReadPacket(serverConn)
			if err != nil {
				return
			}
			switch pkt.ActionID {
			case protocol.ActionIDBeelzebubTrialsCheckCanChallenge:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCheckCanChallenge,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo:
				// 无遗留队伍，允许 AutoChallengeAll 继续建队。
				tw := protocol.NewWriter()
				tw.WriteInt32(0)
				tw.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetPlayerTeamInfo, tw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsOpen:
				// 71 累计次数已达上限；72 今日未挑战且次数未达上限；1 未出现在面板（视为新解锁，保持可挑战）。
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpen,
					beelzebubOpenPayload([]protocol.BeelzebubBossStatus{
						{BossID: 71, Times: 3},
						{BossID: 72, Times: 2},
					}),
				))
			case protocol.ActionIDBeelzebubTrialsGetCanChallengeList:
				bw := protocol.NewWriter()
				bw.WriteUint16(3)
				bw.WriteInt32(71)
				bw.WriteInt32(72)
				bw.WriteInt32(1)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetCanChallengeList, bw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsCreateTeam:
				r := protocol.NewReader(pkt.Payload)
				bossID, _ := r.ReadInt32()
				mu.Lock()
				createdBosses = append(createdBosses, bossID)
				mu.Unlock()
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsCreateTeam,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsGetHelperRoleList:
				hw := protocol.NewWriter()
				hw.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetHelperRoleList, hw.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsFight:
				// 单向通知，不回包。
			case protocol.ActionIDBeelzebubTrialsGetWarResult:
				ww := protocol.NewWriter()
				ww.WriteUint8(protocol.BeelzebubTrialsSuccess)
				ww.WriteInt32(0)
				ww.WriteInt32(0)
				ww.WriteUint16(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsGetWarResult, ww.Bytes(),
				))
			case protocol.ActionIDBeelzebubTrialsLeaveWar:
				// 单向离场通知，不回包。
			case protocol.ActionIDBeelzebubTrialsExitTeam:
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsExitTeam,
					[]byte{protocol.BeelzebubTrialsSuccess},
				))
			case protocol.ActionIDBeelzebubTrialsOpenMoral:
				mw := protocol.NewWriter()
				mw.WriteInt32(0)
				mw.WriteInt32(0)
				mw.WriteInt32(0)
				_ = protocol.WritePacket(serverConn, protocol.NewPacket(
					protocol.ActionIDBeelzebubTrialsOpenMoral, mw.Bytes(),
				))
			}
		}
	}()

	s := NewRoleSession(SessionConfig{
		ServerAddr: "sandbox",
		RoleID:     "1001",
		Dialer: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return clientConn, nil
		},
		Authenticator: func(s *RoleSession) error {
			return nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("session start failed: %v", err)
	}
	defer s.Close()

	deadline := time.Now().Add(2 * time.Second)
	for s.State() != StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	results, err := s.BeelzebubTrialsAutoChallengeAll(ctx, 0)
	if err != nil {
		t.Fatalf("auto challenge failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 challenge results (72 + newly unlocked 1), got %d", len(results))
	}

	mu.Lock()
	defer mu.Unlock()
	want := []int32{72, 1}
	if len(createdBosses) != len(want) {
		t.Fatalf("expected create_team for %v, got %v", want, createdBosses)
	}
	for i := range want {
		if createdBosses[i] != want[i] {
			t.Fatalf("expected create_team order %v, got %v", want, createdBosses)
		}
	}
}
