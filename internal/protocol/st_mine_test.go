// Package protocol_test 提供仙界矿山协议 (Module 264) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestStMineInfoResult(t *testing.T) {
	info, err := protocol.ParseStMineInfoResult(protocol.BuildStMineInfoResultPacket(protocol.StMineInfo{
		Result: protocol.StMineResultSuccess, Level: 2, CurMountain: 3,
	}).Payload)
	if err != nil || info.Result != protocol.StMineResultSuccess || info.Level != 2 || info.CurMountain != 3 {
		t.Fatalf("矿山信息不一致: %+v err=%v", info, err)
	}
}

func TestStMineActionResult(t *testing.T) {
	result, err := protocol.ParseStMineActionResult(protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineEnter, protocol.StMineResultSuccess).Payload)
	if err != nil || result != protocol.StMineResultSuccess {
		t.Fatalf("期望成功码 9, got %d err=%v", result, err)
	}
	// 失败码: 功能未开放
	result, err = protocol.ParseStMineActionResult(protocol.BuildStMineActionResultPacket(protocol.ActionIDStMineEnter, protocol.StMineResultFunctionUnopen).Payload)
	if err != nil || result != protocol.StMineResultFunctionUnopen {
		t.Fatalf("期望未开放码 %d, got %d err=%v", protocol.StMineResultFunctionUnopen, result, err)
	}
	if _, err := protocol.ParseStMineActionResult(nil); err == nil {
		t.Error("空矿山回包应返回解析错误")
	}
}

func TestStMineEnterResponseRoundTrip(t *testing.T) {
	in := protocol.StMineHolesInfo{
		Result:           protocol.StMineResultSuccess,
		SkillLevel:       3,
		SkillValue:       120,
		RemainRobCount:   4,
		BuyNeedIngot:     20,
		NextRobTimestamp: 1794670000,
		PlayerHole: protocol.StMinePlayerHole{
			Status:               33,
			HoleID:               12,
			NextAwardTimestamp:   1794673600,
			ProtectOverTimestamp: 1794677200,
			ItemID:               1001,
			ItemCount:            30,
			NeedMinutes:          60,
			ReceiveCount:         7,
			RecaptureTimestamp:   1794671111,
			EnemyRoleID:          888,
			EnemyNickname:        "敌人",
			EnemyServername:      "s1",
		},
		Holes: []protocol.StMineHole{
			{HoleID: 7, Status: protocol.StMineHoleEmpty},
			{HoleID: 9, Status: protocol.StMineHoleRob, PlayerID: 888, NickName: "对手", PlayerLevel: 120, ServerName: "s2", ReceiveCount: 3},
		},
		FeedbackMult: 5,
	}
	got, err := protocol.ParseStMineEnterResponse(protocol.BuildStMineEnterResultPacket(in).Payload)
	if err != nil {
		t.Fatalf("ParseStMineEnterResponse: %v", err)
	}
	if got.Result != in.Result || got.SkillLevel != 3 || got.SkillValue != 120 ||
		got.RemainRobCount != 4 || got.NextRobTimestamp != in.NextRobTimestamp {
		t.Fatalf("头部字段不一致: %+v", got)
	}
	if got.PlayerHole.HoleID != 12 || got.PlayerHole.ReceiveCount != 7 ||
		got.PlayerHole.Status != 33 || got.PlayerHole.EnemyNickname != "敌人" {
		t.Fatalf("玩家矿洞字段不一致: %+v", got.PlayerHole)
	}
	if len(got.Holes) != 2 || got.Holes[1].NickName != "对手" ||
		got.Holes[1].PlayerID != 888 || got.Holes[1].ReceiveCount != 3 {
		t.Fatalf("矿洞列表不一致: %+v", got.Holes)
	}
	if got.FeedbackMult != 5 {
		t.Fatalf("feedback_mult = %d, want 5", got.FeedbackMult)
	}
}

func TestStMineRobHolePacket(t *testing.T) {
	pkt := protocol.BuildStMineRobHolePacket(7, 888)
	if pkt.ActionID != protocol.ActionIDStMineRobHole || len(pkt.Payload) != 6 {
		t.Fatalf("264/3 封包不符: %+v", pkt)
	}
	if pkt.Payload[0] != 0 || pkt.Payload[1] != 7 {
		t.Fatalf("hole_id 编码不符: %x", pkt.Payload)
	}
	if got := int32(pkt.Payload[2])<<24 | int32(pkt.Payload[3])<<16 | int32(pkt.Payload[4])<<8 | int32(pkt.Payload[5]); got != 888 {
		t.Fatalf("robbed_player_id = %d, want 888", got)
	}
}
