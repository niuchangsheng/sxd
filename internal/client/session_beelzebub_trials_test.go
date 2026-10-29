package client

import (
	"context"
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
