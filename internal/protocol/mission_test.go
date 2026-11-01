// Package protocol_test 提供神仙道关卡协议封包的单元测试。
//
// @author Ateng
// @since 2026-10-08
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestMission_SweepPacket(t *testing.T) {
	req := protocol.SweepRequest{
		MissionID: 105,
		Times:     10,
	}

	pkt, err := protocol.BuildSweepPacket(req)
	if err != nil {
		t.Fatalf("BuildSweepPacket 失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionMissionSweep {
		t.Errorf("期望 ActionID 为 0x%04X, 实际为 0x%04X", protocol.ActionMissionSweep, pkt.ActionID)
	}

	parsedReq, err := protocol.ParseSweepRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseSweepRequest 失败: %v", err)
	}
	if parsedReq.MissionID != req.MissionID || parsedReq.Times != req.Times {
		t.Errorf("解析请求不一致: 期望 %+v, 实际 %+v", req, *parsedReq)
	}

	res := protocol.SweepResult{
		Success:   true,
		MissionID: 105,
		Times:     10,
		CostPower: 50,
		GainExp:   25000,
		GainCoins: 120000,
		Message:   "扫荡完毕",
	}

	resPkt, err := protocol.BuildSweepResultPacket(res)
	if err != nil {
		t.Fatalf("BuildSweepResultPacket 失败: %v", err)
	}

	parsedRes, err := protocol.ParseSweepResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("ParseSweepResult 失败: %v", err)
	}
	if !parsedRes.Success || parsedRes.CostPower != 50 || parsedRes.GainCoins != 120000 || parsedRes.Message != "扫荡完毕" {
		t.Errorf("解析结算不一致: %+v", *parsedRes)
	}
}

// TestMission_FightMonsterPacket_LiveFormat 锁定 2026 网关 13 字节 fight_monster 载荷布局。
// 9 字节变体 (省略 hireRoleID) 会被服务端静默丢弃，必须保留 Int32 hireRoleID 占位。
func TestMission_FightMonsterPacket_LiveFormat(t *testing.T) {
	pkt := protocol.BuildMissionFightMonsterPacket(954, 0, 0, protocol.ActionIDMissionPlotEnter)
	if pkt.ActionID != protocol.ActionIDMissionFightMonster {
		t.Fatalf("ActionID 不匹配: %d", pkt.ActionID)
	}
	const wantHex = "000003ba000000000000040001"
	gotHex := hex.EncodeToString(pkt.Payload)
	if gotHex != wantHex {
		t.Errorf("fight_monster 载荷不匹配: 期望 %s, 实际 %s", wantHex, gotHex)
	}
	if len(pkt.Payload) != 13 {
		t.Errorf("fight_monster 载荷长度应为 13, 实际 %d", len(pkt.Payload))
	}
}

func TestMission_LoginPacket(t *testing.T) {
	pkt, err := protocol.BuildLoginPacket("梦一场", "mock-auth-token-123")
	if err != nil {
		t.Fatalf("BuildLoginPacket 失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionPlayerLogin {
		t.Errorf("期望 ActionID 为 0x%04X, 实际为 0x%04X", protocol.ActionPlayerLogin, pkt.ActionID)
	}

	name, tok, err := protocol.ParseLoginPacket(pkt.Payload)
	if err != nil {
		t.Fatalf("ParseLoginPacket 失败: %v", err)
	}
	if name != "梦一场" || tok != "mock-auth-token-123" {
		t.Errorf("解析登录封包不匹配: name=%s, token=%s", name, tok)
	}
}
