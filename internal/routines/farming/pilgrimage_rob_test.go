package farming

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
)

// buildRoadPayload 构造 open_take_bible 回包，仅包含打劫流程所需字段。
func buildRoadPayload(teams []protocol.StTakeBibleCrossRoadTeam, canRobTimes int16) []byte {
	w := protocol.NewWriter()
	w.WriteUint16(uint16(len(teams)))
	for _, team := range teams {
		w.WriteInt32(team.PlayerID)
		w.WriteUint8(team.Protection)
		w.WriteInt32(team.StartTime)
		w.WriteInt32(team.ArrivalTime)
		w.WriteInt16(team.Distance)
		w.WriteInt32(team.ProtectPlayerID)
		w.WriteInt8(team.SequenceID)
		w.WriteInt8(team.IsDeedsSo)
	}
	w.WriteInt32(0)
	w.WriteInt16(canRobTimes)
	w.WriteInt8(0)
	w.WriteInt8(0)
	w.WriteInt8(0)
	w.WriteInt8(0)
	w.WriteUint8(0)
	w.WriteInt32(0)
	w.WriteUint16(0)
	w.WriteInt8(0)
	return w.Bytes()
}

// buildPlayerInfoPayload 构造 get_player_take_bible 回包。
func buildPlayerInfoPayload(info protocol.StTakeBibleCrossPlayerInfo) []byte {
	w := protocol.NewWriter()
	w.WriteInt32(info.PlayerID)
	w.WriteString(info.NickName)
	w.WriteString(info.StageName)
	w.WriteString(info.ServerName)
	w.WriteInt16(info.Level)
	w.WriteInt8(info.BeRobTimes)
	w.WriteInt8(info.TotalCanRobTimes)
	w.WriteString(info.ProtectNickname)
	w.WriteInt32(info.RobCoin)
	w.WriteInt16(info.RobFame)
	w.WriteInt16(info.RobStExp)
	w.WriteInt32(info.AwardCoin)
	w.WriteInt16(info.AwardFame)
	w.WriteInt16(info.AwardStExp)
	w.WriteInt8(info.IsRobbed)
	w.WriteInt16(info.ProtectTime)
	w.WriteInt8(info.NPCLevel)
	w.WriteInt16(info.RobExpe)
	w.WriteInt16(info.AwardExpe)
	w.WriteInt8(info.SequenceID)
	return w.Bytes()
}

// buildRobResultPayload 构造 rob 回包前缀字段。
func buildRobResultPayload(result protocol.StTakeBibleCrossRobResult) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(result.Msg)
	w.WriteUint16(1)
	w.WriteInt32(result.TargetPlayerID)
	w.WriteString(result.TargetNickName)
	w.WriteString(result.TargetStageName)
	w.WriteString(result.TargetServerName)
	w.WriteInt32(result.RobCoin)
	w.WriteInt16(result.RobFame)
	w.WriteInt16(result.RobStExp)
	w.WriteInt32(result.WarReportID)
	// 战报单元列表：官方 WarData 以 Uint16 单元数开头，单元前缀为
	// Uint8 deploy_type, Int32 win_player_id。
	if result.WarParsed {
		w.WriteUint16(1)
		w.WriteUint8(result.DeployType)
		w.WriteInt32(result.WinPlayerID)
	}
	return w.Bytes()
}

func buildRobCDPayload(seconds, ingot int16) []byte {
	w := protocol.NewWriter()
	w.WriteInt16(seconds)
	w.WriteInt16(ingot)
	return w.Bytes()
}

func newRobTestSession(playerID int32, level int) *client.RoleSession {
	session := client.NewRoleSession(client.SessionConfig{RoleID: "role-rob-test"})
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.PlayerID = playerID
		ps.Level = level
	})
	return session
}

func TestCrossPilgrimageRob_SuccessThenCooldown(t *testing.T) {
	session := newRobTestSession(1000, 180)
	initialCoins := session.GetPlayerState().Coins

	var openCalls, cdCalls, playerCalls, robCalls, closeCalls int
	cdValues := []int16{0, 30}
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			openCalls++
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1000, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionSunWuKong, SequenceID: 2},
			}, 3)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			cdCalls++
			seconds := int16(0)
			if len(cdValues) > 0 {
				seconds = cdValues[0]
				cdValues = cdValues[1:]
			}
			return protocol.NewPacket(expectedAct, buildRobCDPayload(seconds, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			playerCalls++
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         1001,
				NickName:         "曹植始",
				SequenceID:       2,
				Level:            180,
				BeRobTimes:       0,
				TotalCanRobTimes: 3,
				RobCoin:          5000,
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgSuccess,
				TargetPlayerID: 1001,
				TargetNickName: "曹植始",
				RobCoin:        5000,
				RobFame:        20,
				RobStExp:       10,
				WarReportID:    9001,
			})), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			closeCalls++
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if openCalls != 1 || cdCalls != 2 || playerCalls != 1 || robCalls != 1 || closeCalls != 1 {
		t.Fatalf("unexpected call counts: open=%d cd=%d player=%d rob=%d close=%d",
			openCalls, cdCalls, playerCalls, robCalls, closeCalls)
	}
	if got := session.GetPlayerState().Coins; got != initialCoins+5000 {
		t.Errorf("expected coins %d after reward, got %d", initialCoins+5000, got)
	}
}

func TestCrossPilgrimageRob_SkipsWhenCoolingDown(t *testing.T) {
	session := newRobTestSession(1000, 180)

	var playerCalls, robCalls, closeCalls int
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
			}, 3)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			return protocol.NewPacket(expectedAct, buildRobCDPayload(45, 20)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			playerCalls++
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
		case protocol.ActionIDStTakeBibleCrossClose:
			closeCalls++
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if playerCalls != 0 || robCalls != 0 {
		t.Errorf("expected no player probe/rob while cooling down, got player=%d rob=%d", playerCalls, robCalls)
	}
	if closeCalls != 1 {
		t.Errorf("expected road panel closed once, got %d", closeCalls)
	}
}

func TestCrossPilgrimageRob_FailedRobEntersCooldown(t *testing.T) {
	session := newRobTestSession(1000, 180)

	var cdCalls, playerCalls, robCalls, closeCalls int
	cdValues := []int16{0, 45}
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionShaWuJing, SequenceID: 1},
			}, 4)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			seconds := int16(0)
			if len(cdValues) > 0 {
				seconds = cdValues[0]
				cdValues = cdValues[1:]
			}
			cdCalls++
			return protocol.NewPacket(expectedAct, buildRobCDPayload(seconds, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			info := protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         1001,
				NickName:         "已打过",
				SequenceID:       1,
				RobCoin:          5000,
				TotalCanRobTimes: 3,
				RobFame:          20,
				RobStExp:         10,
			}
			if playerCalls == 1 {
				info.PlayerID = 1002
				info.NickName = "次选目标"
				info.RobCoin = 1000
			}
			playerCalls++
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(info)), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			// 首次打劫失败（重复打劫），服务端同样会进入冷却。
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgRerob,
				TargetPlayerID: 1001,
				TargetNickName: "已打过",
			})), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			closeCalls++
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 1 {
		t.Errorf("expected to stop after failed rob cooldown, got %d rob calls", robCalls)
	}
	if cdCalls != 2 {
		t.Errorf("expected cooldown checked before and after failed rob, got %d calls", cdCalls)
	}
	if playerCalls != 2 {
		t.Errorf("expected 2 target probes before robbing, got %d", playerCalls)
	}
	if closeCalls != 1 {
		t.Errorf("expected road panel closed once, got %d", closeCalls)
	}
}

func TestCrossPilgrimageRob_SkipsRobbedAndExhaustedTargets(t *testing.T) {
	session := newRobTestSession(1000, 180)

	var playerCalls, robCalls int
	infos := []protocol.StTakeBibleCrossPlayerInfo{
		{PlayerID: 1001, NickName: "已打劫", SequenceID: 1, IsRobbed: 1, RobCoin: 5000},
		{PlayerID: 1002, NickName: "次数用尽", SequenceID: 1, BeRobTimes: 3, TotalCanRobTimes: 3, RobCoin: 5000},
		{PlayerID: 1003, NickName: "保护中", SequenceID: 1, ProtectTime: 60, RobCoin: 5000},
	}
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1003, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
			}, 3)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 0)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			info := infos[playerCalls]
			playerCalls++
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(info)), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if playerCalls != 3 {
		t.Errorf("expected 3 probes, got %d", playerCalls)
	}
	if robCalls != 0 {
		t.Errorf("expected no rob for invalid targets, got %d", robCalls)
	}
}

// TestCrossPilgrimageRob_TimeoutThenCooldownTreatedAsSettled 覆盖 rob 回包超时后的受理复核：
// 冷却已被触发说明服务端已完成结算，必须按已受理收手，不得重复打劫同一轮目标。
func TestCrossPilgrimageRob_TimeoutThenCooldownTreatedAsSettled(t *testing.T) {
	session := newRobTestSession(1000, 180)
	initialCoins := session.GetPlayerState().Coins

	var cdCalls, robCalls int
	cdValues := []int16{0, 600}
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionSunWuKong, SequenceID: 1},
			}, 4)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			seconds := int16(0)
			if len(cdValues) > 0 {
				seconds = cdValues[0]
				cdValues = cdValues[1:]
			}
			cdCalls++
			return protocol.NewPacket(expectedAct, buildRobCDPayload(seconds, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			playerID := int32(1001)
			if playerCallsFromPacket(pkt) == 1002 {
				playerID = 1002
			}
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         playerID,
				NickName:         "可劫目标",
				SequenceID:       1,
				TotalCanRobTimes: 3,
				RobCoin:          5000,
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			// 服务端已完成战斗并触发冷却，但战报回包在预算内没有送达。
			return nil, context.DeadlineExceeded
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 1 {
		t.Errorf("expected exactly 1 rob attempt after timeout, got %d", robCalls)
	}
	if cdCalls != 2 {
		t.Errorf("expected cooldown checked before rob and after timeout, got %d", cdCalls)
	}
	if got := session.GetPlayerState().Coins; got != initialCoins {
		t.Errorf("timeout reconciliation must not credit rewards, coins %d -> %d", initialCoins, got)
	}
}

// TestCrossPilgrimageRob_TimeoutThenNoCooldownSkipsTarget 覆盖 rob 回包超时且冷却仍为 0：
// 请求未被受理，应跳过该目标继续尝试后续目标，而不是中断整轮打劫。
func TestCrossPilgrimageRob_TimeoutThenNoCooldownSkipsTarget(t *testing.T) {
	session := newRobTestSession(1000, 180)
	initialCoins := session.GetPlayerState().Coins

	var cdCalls, robCalls int
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionSunWuKong, SequenceID: 1},
			}, 4)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			cdCalls++
			return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         playerCallsFromPacket(pkt),
				NickName:         "可劫目标",
				SequenceID:       1,
				TotalCanRobTimes: 3,
				RobCoin:          5000,
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			if robCalls == 1 {
				// 首个目标静默丢包且未触发冷却。
				return nil, context.DeadlineExceeded
			}
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgSuccess,
				TargetPlayerID: playerCallsFromPacket(pkt),
				TargetNickName: "可劫目标",
				RobCoin:        5000,
				RobFame:        20,
				RobStExp:       10,
				WarReportID:    9002,
			})), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 2 {
		t.Errorf("expected to skip timed-out target and rob the next one, got %d rob calls", robCalls)
	}
	if cdCalls != 3 {
		t.Errorf("expected 3 cooldown checks (before, reconcile, after success), got %d", cdCalls)
	}
	if got := session.GetPlayerState().Coins; got != initialCoins+5000 {
		t.Errorf("expected only the settled rob reward, coins %d -> %d", initialCoins, got)
	}
}

// TestCrossPilgrimageRob_TimeoutThenReconcileFailsStops 覆盖 rob 回包超时且冷却复核也失败：
// 状态未知时必须保守结束本轮，避免在未知结算态下继续打劫。
func TestCrossPilgrimageRob_TimeoutThenReconcileFailsStops(t *testing.T) {
	session := newRobTestSession(1000, 180)

	var cdCalls, robCalls int
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionSunWuKong, SequenceID: 1},
			}, 4)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			cdCalls++
			if cdCalls == 1 {
				return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 10)), nil
			}
			return nil, context.DeadlineExceeded
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         playerCallsFromPacket(pkt),
				NickName:         "可劫目标",
				SequenceID:       1,
				TotalCanRobTimes: 3,
				RobCoin:          5000,
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			return nil, context.DeadlineExceeded
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 1 {
		t.Errorf("expected to stop after unknown settlement state, got %d rob calls", robCalls)
	}
	if cdCalls != 2 {
		t.Errorf("expected initial cooldown check plus one reconcile, got %d", cdCalls)
	}
}

func buildUpdateDataPayload(canRobTimes int16, canTakeTimes uint8) []byte {
	w := protocol.NewWriter()
	w.WriteInt16(canRobTimes)
	w.WriteUint8(canTakeTimes)
	return w.Bytes()
}

// TestCrossPilgrimageRob_PrefersLowestLevelTarget 覆盖软柿子策略：
// 打劫失败不扣每日次数，因此必须优先攻击等级最低的可劫目标，而不是收益最高者。
func TestCrossPilgrimageRob_PrefersLowestLevelTarget(t *testing.T) {
	session := newRobTestSession(1000, 70)

	var robOrder []int32
	levels := map[int32]int16{1001: 180, 1002: 55}
	coins := map[int32]int32{1001: 500000, 1002: 5000}
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
				{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionBaiLongMa, SequenceID: 1},
			}, 4)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			pid := playerCallsFromPacket(pkt)
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         pid,
				NickName:         "目标",
				SequenceID:       1,
				Level:            levels[pid],
				TotalCanRobTimes: 3,
				RobCoin:          coins[pid],
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			pid := playerCallsFromPacket(pkt)
			robOrder = append(robOrder, pid)
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgSuccess,
				TargetPlayerID: pid,
				RobCoin:        coins[pid],
				RobFame:        20,
				RobStExp:       10,
				WarReportID:    9001,
				DeployType:     3,
				WinPlayerID:    1000,
				WarParsed:      true,
			})), nil
		case protocol.ActionIDStTakeBibleCrossUpdateData:
			return protocol.NewPacket(expectedAct, buildUpdateDataPayload(3, 0)), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	// 等级 180 的目标超过自身 70+10，直接跳过；只打等级 55 的软柿子。
	if len(robOrder) != 1 || robOrder[0] != 1002 {
		t.Fatalf("expected only soft target 1002 (1001 too high level), got %v", robOrder)
	}
}

// TestCrossPilgrimageRob_LossDoesNotCreditOrConsume 覆盖 msg=SUCCESS 但战斗失败：
// 战报胜负位指向对手时必须如实记录为失败，不能发放奖励，也不能计入成功次数。
func TestCrossPilgrimageRob_LossDoesNotCreditOrConsume(t *testing.T) {
	session := newRobTestSession(1000, 70)
	initialCoins := session.GetPlayerState().Coins

	var cdCalls, robCalls int
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload([]protocol.StTakeBibleCrossRoadTeam{
				{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
			}, 5)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			cdCalls++
			return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         1001,
				NickName:         "强敌",
				SequenceID:       1,
				Level:            75, // 自身 70 + 10 以内，允许发起并战败
				TotalCanRobTimes: 3,
				RobCoin:          500000,
				RobFame:          120,
				RobStExp:         80,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			// 服务端受理并完成战斗，但本方战败：奖励全 0，胜负位指向对手。
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgSuccess,
				TargetPlayerID: 1001,
				TargetNickName: "强敌",
				WarReportID:    9003,
				DeployType:     3,
				WinPlayerID:    1001,
				WarParsed:      true,
			})), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 1 || cdCalls != 2 {
		t.Fatalf("expected 1 rob and 2 cooldown checks, got rob=%d cd=%d", robCalls, cdCalls)
	}
	if got := session.GetPlayerState().Coins; got != initialCoins {
		t.Errorf("defeated rob must not credit coins, %d -> %d", initialCoins, got)
	}
}

// TestCrossPilgrimageRob_UsesFiveDailyAttempts 覆盖每日 5 次上限：
// 每次获胜后服务端同步剩余次数，连续获胜时应能完整消耗 5 次。
func TestCrossPilgrimageRob_UsesFiveDailyAttempts(t *testing.T) {
	session := newRobTestSession(1000, 70)

	targets := make([]protocol.StTakeBibleCrossRoadTeam, 0, 6)
	for i := int32(0); i < 6; i++ {
		targets = append(targets, protocol.StTakeBibleCrossRoadTeam{
			PlayerID:   1001 + i,
			Protection: protocol.StTakeBibleCrossProtectionTangSeng,
			SequenceID: 1,
		})
	}
	remaining := int16(4)
	robCalls := 0
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload(targets, 5)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			pid := playerCallsFromPacket(pkt)
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         pid,
				NickName:         "可劫目标",
				SequenceID:       1,
				Level:            60,
				TotalCanRobTimes: 3,
				RobCoin:          5000,
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			pid := playerCallsFromPacket(pkt)
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgSuccess,
				TargetPlayerID: pid,
				RobCoin:        5000,
				RobFame:        20,
				RobStExp:       10,
				WarReportID:    int32(9100 + robCalls),
				DeployType:     3,
				WinPlayerID:    1000,
				WarParsed:      true,
			})), nil
		case protocol.ActionIDStTakeBibleCrossUpdateData:
			current := remaining
			if remaining > 0 {
				remaining--
			}
			return protocol.NewPacket(expectedAct, buildUpdateDataPayload(current, 0)), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 5 {
		t.Fatalf("expected 5 daily rob attempts, got %d", robCalls)
	}
}

// TestCrossPilgrimageRob_StopsWhenServerReportsNoRemainingTimes 覆盖服务端剩余次数
// 与路况快照不一致的场景：路况仍显示 5 次，但 updata_data 已下发 0 次时必须立即收手。
func TestCrossPilgrimageRob_StopsWhenServerReportsNoRemainingTimes(t *testing.T) {
	session := newRobTestSession(1000, 70)

	targets := []protocol.StTakeBibleCrossRoadTeam{
		{PlayerID: 1001, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
		{PlayerID: 1002, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
		{PlayerID: 1003, Protection: protocol.StTakeBibleCrossProtectionTangSeng, SequenceID: 1},
	}
	robCalls := 0
	crossCall := func(pkt *protocol.Packet, expectedAct uint32, _ time.Duration) (*protocol.Packet, error) {
		switch expectedAct {
		case protocol.ActionIDStTakeBibleCrossOpen:
			return protocol.NewPacket(expectedAct, buildRoadPayload(targets, 5)), nil
		case protocol.ActionIDStTakeBibleCrossGetRobCD:
			return protocol.NewPacket(expectedAct, buildRobCDPayload(0, 10)), nil
		case protocol.ActionIDStTakeBibleCrossGetPlayer:
			pid := playerCallsFromPacket(pkt)
			return protocol.NewPacket(expectedAct, buildPlayerInfoPayload(protocol.StTakeBibleCrossPlayerInfo{
				PlayerID:         pid,
				NickName:         "可劫目标",
				SequenceID:       1,
				Level:            60,
				TotalCanRobTimes: 3,
				RobCoin:          5000,
				RobFame:          20,
				RobStExp:         10,
			})), nil
		case protocol.ActionIDStTakeBibleCrossRob:
			robCalls++
			pid := playerCallsFromPacket(pkt)
			return protocol.NewPacket(expectedAct, buildRobResultPayload(protocol.StTakeBibleCrossRobResult{
				Msg:            protocol.StTakeBibleCrossMsgSuccess,
				TargetPlayerID: pid,
				RobCoin:        5000,
				RobFame:        20,
				RobStExp:       10,
				WarReportID:    9200,
				DeployType:     3,
				WinPlayerID:    1000,
				WarParsed:      true,
			})), nil
		case protocol.ActionIDStTakeBibleCrossUpdateData:
			return protocol.NewPacket(expectedAct, buildUpdateDataPayload(0, 0)), nil
		case protocol.ActionIDStTakeBibleCrossClose:
			return protocol.NewPacket(expectedAct, nil), nil
		}
		return nil, context.DeadlineExceeded
	}

	crossPilgrimageRob(context.Background(), session, session.PlayerID(), crossCall, func(m uint8) uint8 { return m }, DefaultPilgrimageConfig())

	if robCalls != 1 {
		t.Fatalf("expected to stop after server reports 0 remaining times, got %d rob calls", robCalls)
	}
}

// playerCallsFromPacket 从请求载荷的起始 Int32 读取玩家 ID，便于 mock 区分目标。
func playerCallsFromPacket(pkt *protocol.Packet) int32 {
	if pkt == nil || len(pkt.Payload) < 4 {
		return 0
	}
	return int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 | uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3]))
}

// TestCrossPilgrimageEscort_StartsThreeQueues 验证圣域取经三个队列可一次连续启程 3 次。
func TestCrossPilgrimageEscort_StartsThreeQueues(t *testing.T) {
	starts := 0
	infoCalls := 0
	fake := func(pkt *protocol.Packet, act uint32, d time.Duration) (*protocol.Packet, error) {
		switch act {
		case protocol.ActionIDStTakeBibleCrossGetInfo:
			infoCalls++
			w := protocol.NewWriter()
			w.WriteUint16(0)            // protection list
			w.WriteUint16(0)            // protect player list
			w.WriteUint8(uint8(starts)) // TakeBibleTimes
			w.WriteInt8(3)              // TotalTakeBibleTimes
			w.WriteInt16(0)             // RefreshPrice (free)
			w.WriteInt8(0)              // TakeBibleStatus
			w.WriteUint8(5)             // CanProtection = TangSeng
			w.WriteString("")           // ProtectPlayer
			w.WriteInt8(0).WriteInt8(0) // Bless, Buff
			return protocol.NewPacket(act, w.Bytes()), nil
		case protocol.ActionIDStTakeBibleCrossStart:
			starts++
			w := protocol.NewWriter()
			w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
			w.WriteInt8(1)
			return protocol.NewPacket(act, w.Bytes()), nil
		}
		return nil, fmt.Errorf("unexpected action 0x%08X", act)
	}
	sess := client.NewRoleSession(client.SessionConfig{RoleID: "role-pilgrimage-3queues"})
	crossPilgrimageEscort(context.Background(), sess, fake, func(m uint8) uint8 { return m })
	if starts != 3 || infoCalls != 3 {
		t.Fatalf("应连续启程 3 次并在每次前刷新详情, starts=%d info_calls=%d", starts, infoCalls)
	}
}

// TestCrossPilgrimageEscort_StopsWhenTimesFull 验证剩余次数不足时按实际剩余启程。
func TestCrossPilgrimageEscort_StopsWhenTimesFull(t *testing.T) {
	starts := 0
	infoCalls := 0
	fake := func(pkt *protocol.Packet, act uint32, d time.Duration) (*protocol.Packet, error) {
		switch act {
		case protocol.ActionIDStTakeBibleCrossGetInfo:
			infoCalls++
			w := protocol.NewWriter()
			w.WriteUint16(0)
			w.WriteUint16(0)
			w.WriteUint8(uint8(2 + starts)) // 已用 2 次，剩余 1 次
			w.WriteInt8(3)
			w.WriteInt16(0)
			w.WriteInt8(0)
			w.WriteUint8(5)
			w.WriteString("")
			w.WriteInt8(0).WriteInt8(0)
			return protocol.NewPacket(act, w.Bytes()), nil
		case protocol.ActionIDStTakeBibleCrossStart:
			starts++
			w := protocol.NewWriter()
			w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
			w.WriteInt8(1)
			return protocol.NewPacket(act, w.Bytes()), nil
		}
		return nil, fmt.Errorf("unexpected action 0x%08X", act)
	}
	sess := client.NewRoleSession(client.SessionConfig{RoleID: "role-pilgrimage-1left"})
	crossPilgrimageEscort(context.Background(), sess, fake, func(m uint8) uint8 { return m })
	if starts != 1 || infoCalls != 2 {
		t.Fatalf("剩余 1 次应只启程 1 次, starts=%d info_calls=%d", starts, infoCalls)
	}
}
