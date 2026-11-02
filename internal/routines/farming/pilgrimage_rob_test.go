package farming

import (
	"context"
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

	crossPilgrimageRob(context.Background(), session, crossCall, DefaultPilgrimageConfig())

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

	crossPilgrimageRob(context.Background(), session, crossCall, DefaultPilgrimageConfig())

	if playerCalls != 0 || robCalls != 0 {
		t.Errorf("expected no player probe/rob while cooling down, got player=%d rob=%d", playerCalls, robCalls)
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

	crossPilgrimageRob(context.Background(), session, crossCall, DefaultPilgrimageConfig())

	if playerCalls != 3 {
		t.Errorf("expected 3 probes, got %d", playerCalls)
	}
	if robCalls != 0 {
		t.Errorf("expected no rob for invalid targets, got %d", robCalls)
	}
}
