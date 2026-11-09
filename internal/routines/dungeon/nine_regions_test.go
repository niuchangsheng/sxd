// Package dungeon_test 提供九空无界自动化测试。
//
// @author Ateng
// @since 2026-11-08
package dungeon_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
	"sxd-pie-ng/internal/scheduler"
)

func TestNineRegionsRoutine_CreateTeamChooseHelpersAndFight(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var panelCalls, startCalls, createCalls, chooseCalls, fightCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDNineRegionsOpen:
			n := panelCalls.Add(1)
			panel := protocol.NineRegionsPanel{CurJie: 1, CurLevel: 0}
			if n > 1 {
				panel.CurLevel = 1
			}
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsPanelResultPacket(panel))
			return true
		case protocol.ActionIDNineRegionsCD:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCDResultPacket(0))
			return true
		case protocol.ActionIDNineRegionsStartChallenge:
			n := startCalls.Add(1)
			code := uint8(protocol.NineRegionsResultChallengeSuccess)
			if n > 1 {
				code = protocol.NineRegionsResultChallenged
			}
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsStartResultPacket(
				protocol.NineRegionsStartResult{Result: code}))
			return true
		case protocol.ActionIDNineRegionsTeamInfo:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsTeamInfoResultPacket(
				protocol.NineRegionsTeamInfo{LeaderID: 0}))
			return true
		case protocol.ActionIDNineRegionsCreateTeam:
			createCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCreateTeamResultPacket(
				protocol.NineRegionsCreateTeamResult{Result: protocol.NineRegionsResultSuccess, TeamID: 100}))
			return true
		case protocol.ActionIDNineRegionsHelperList:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsHelperListResultPacket([]protocol.NineRegionsHelperRole{
				{RoleID: 77, Name: "仙猪八戒", Level: 80, State: protocol.NineRegionsResultDeploy},
				{RoleID: 75, Name: "仙少年沉香", Level: 50, State: protocol.NineRegionsResultNoDeploy},
			}))
			return true
		case protocol.ActionIDNineRegionsChooseHelper:
			chooseCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsSimpleResultPacket(
				protocol.ActionIDNineRegionsChooseHelper, protocol.NineRegionsResultSuccess))
			return true
		case protocol.ActionIDNineRegionsFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsFightResultPacket(
				protocol.NineRegionsResultSuccess, 2))
			return true
		}
		return false
	})

	if err := dungeon.NewNineRegionsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("nine regions: %v", err)
	}
	if got := fightCalls.Load(); got != 1 {
		t.Fatalf("应挑战 1 次, 实际 %d 次", got)
	}
	if got := createCalls.Load(); got != 1 {
		t.Fatalf("无队伍时应创建 1 次, 实际 %d 次", got)
	}
	if got := chooseCalls.Load(); got != 2 {
		t.Fatalf("应选择 2 名助战, 实际 %d 次", got)
	}
	if got := startCalls.Load(); got != 2 {
		t.Fatalf("推进后应再次校验下一关, 实际 %d 次", got)
	}
}

func TestNineRegionsRoutine_RetriesLossThenExits(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var startCalls, fightCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDNineRegionsOpen:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsPanelResultPacket(
				protocol.NineRegionsPanel{CurJie: 1, CurLevel: 0}))
			return true
		case protocol.ActionIDNineRegionsCD:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCDResultPacket(0))
			return true
		case protocol.ActionIDNineRegionsStartChallenge:
			startCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsStartResultPacket(
				protocol.NineRegionsStartResult{Result: protocol.NineRegionsResultChallengeSuccess}))
			return true
		case protocol.ActionIDNineRegionsTeamInfo:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsTeamInfoResultPacket(
				protocol.NineRegionsTeamInfo{LeaderID: 1, Members: []protocol.NineRegionsTeamMember{{PlayerID: 1}}}))
			return true
		case protocol.ActionIDNineRegionsHelperList:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsHelperListResultPacket(nil))
			return true
		case protocol.ActionIDNineRegionsFight:
			fightCalls.Add(1)
			// 战斗执行成功，但面板始终不推进 = 战斗失败。
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsFightResultPacket(
				protocol.NineRegionsResultSuccess, 1))
			return true
		}
		return false
	})

	if err := dungeon.NewNineRegionsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("nine regions: %v", err)
	}
	// 首次 + 3 次重试 = 4 次战斗。
	if got := fightCalls.Load(); got != 4 {
		t.Fatalf("战斗失败应重试至上限, 期望 4 次, 实际 %d 次", got)
	}
	if got := startCalls.Load(); got != 4 {
		t.Fatalf("每次重试都应重新校验, 期望 4 次, 实际 %d 次", got)
	}
}

func TestNineRegionsRoutine_JieRollover(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var gotJie, gotLevel atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDNineRegionsOpen:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsPanelResultPacket(
				protocol.NineRegionsPanel{CurJie: 18, CurLevel: 10}))
			return true
		case protocol.ActionIDNineRegionsCD:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCDResultPacket(0))
			return true
		case protocol.ActionIDNineRegionsStartChallenge:
			r := protocol.NewReader(pkt.Payload)
			jie, _ := r.ReadInt32()
			level, _ := r.ReadInt32()
			gotJie.Store(jie)
			gotLevel.Store(level)
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsStartResultPacket(
				protocol.NineRegionsStartResult{Result: protocol.NineRegionsResultChallenged}))
			return true
		}
		return false
	})

	if err := dungeon.NewNineRegionsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("nine regions: %v", err)
	}
	if gotJie.Load() != 19 || gotLevel.Load() != 1 {
		t.Fatalf("满 10 关后应挑战下一界第 1 关, 实际 jie=%d level=%d", gotJie.Load(), gotLevel.Load())
	}
}

func TestNineRegionsRoutine_CDSkips(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var startCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDNineRegionsOpen:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsPanelResultPacket(
				protocol.NineRegionsPanel{CurJie: 1, CurLevel: 0}))
			return true
		case protocol.ActionIDNineRegionsCD:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCDResultPacket(30))
			return true
		case protocol.ActionIDNineRegionsStartChallenge:
			startCalls.Add(1)
			return true
		}
		return false
	})

	err := dungeon.NewNineRegionsRoutine().Execute(context.Background(), sess, nil)
	var cdErr *scheduler.CooldownError
	if !errors.As(err, &cdErr) {
		t.Fatalf("CD 中应返回 CooldownError, 实际: %v", err)
	}
	if got := startCalls.Load(); got != 0 {
		t.Fatalf("CD 中不应开始挑战, 实际 %d 次", got)
	}
}

func TestNineRegionsRoutine_StartRejectedStops(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var fightCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDNineRegionsOpen:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsPanelResultPacket(
				protocol.NineRegionsPanel{CurJie: 1, CurLevel: 0}))
			return true
		case protocol.ActionIDNineRegionsCD:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCDResultPacket(0))
			return true
		case protocol.ActionIDNineRegionsStartChallenge:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsStartResultPacket(
				protocol.NineRegionsStartResult{Result: protocol.NineRegionsResultChallengeTower}))
			return true
		case protocol.ActionIDNineRegionsFight:
			fightCalls.Add(1)
			return true
		}
		return false
	})

	if err := dungeon.NewNineRegionsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("nine regions: %v", err)
	}
	if got := fightCalls.Load(); got != 0 {
		t.Fatalf("开始挑战被拒绝时不得战斗, 实际 %d 次", got)
	}
}

func TestNineRegionsRoutine_HiddenLevelAttempted(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var hiddenCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDNineRegionsOpen:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsPanelResultPacket(
				protocol.NineRegionsPanel{CurJie: 1, CurLevel: 0, HiddenLevel: 7}))
			return true
		case protocol.ActionIDNineRegionsCD:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsCDResultPacket(0))
			return true
		case protocol.ActionIDNineRegionsStartChallenge:
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsStartResultPacket(
				protocol.NineRegionsStartResult{Result: protocol.NineRegionsResultChallenged}))
			return true
		case protocol.ActionIDNineRegionsHidden:
			hiddenCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildNineRegionsSimpleResultPacket(
				protocol.ActionIDNineRegionsHidden, protocol.NineRegionsResultCantChallengeHidden))
			return true
		}
		return false
	})

	if err := dungeon.NewNineRegionsRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("nine regions: %v", err)
	}
	if got := hiddenCalls.Load(); got != 1 {
		t.Fatalf("存在隐藏关卡时应尝试 1 次, 实际 %d 次", got)
	}
}
