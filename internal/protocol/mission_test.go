// Package protocol_test 提供神仙道关卡协议封包的单元测试。
//
// @author Ateng
// @since 2026-10-08
package protocol_test

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestHeroMissionList_OfficialSchema 验证 Mod_HeroMission_Base.get_hero_mission_list
// 的官方响应布局: [[Int,Byte,UByte,Byte],Byte,Byte,Short,Int]。
func TestHeroMissionList_OfficialSchema(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint16(2) // 条目数
	w.WriteInt32(166)
	w.WriteInt8(1)
	w.WriteUint8(0)
	w.WriteInt8(1)
	w.WriteInt32(167)
	w.WriteInt8(0)
	w.WriteUint8(2)
	w.WriteInt8(0)
	w.WriteInt8(3) // AllBuyTimes
	w.WriteInt8(2) // RemainBuyTimes
	w.WriteInt16(20)
	w.WriteInt32(15)

	list, err := protocol.ParseHeroMissionListResponse(w.Bytes())
	if err != nil {
		t.Fatalf("ParseHeroMissionListResponse 失败: %v", err)
	}
	if len(list.Missions) != 2 {
		t.Fatalf("期望 2 个英雄副本条目, 实际 %d", len(list.Missions))
	}
	if list.Missions[0] != (protocol.HeroMissionItem{MissionID: 166, CanChallenge: 1, Rank: 0, Finished: 1}) {
		t.Errorf("条目 0 解析不一致: %+v", list.Missions[0])
	}
	if list.Missions[1] != (protocol.HeroMissionItem{MissionID: 167, CanChallenge: 0, Rank: 2, Finished: 0}) {
		t.Errorf("条目 1 解析不一致: %+v", list.Missions[1])
	}
	if list.AllBuyTimes != 3 || list.RemainBuyTimes != 2 || list.BuyPrice != 20 || list.FinishMissionNum != 15 {
		t.Errorf("列表状态解析不一致: %+v", *list)
	}
}

// TestHeroMissionListPacket_RequestLayout 验证列表请求载荷:
// [town_id u16][PrevAct 4 字节占位]，并标记 HasPrevAct 由会话层动态覆盖。
func TestHeroMissionListPacket_RequestLayout(t *testing.T) {
	pkt := protocol.BuildHeroMissionListPacket(13)
	if pkt.ActionID != protocol.ActionHeroMissionList {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionHeroMissionList, pkt.ActionID)
	}
	if len(pkt.Payload) != 6 {
		t.Fatalf("期望 6 字节载荷 (town_id + PrevAct), 实际 %d 字节", len(pkt.Payload))
	}
	if !pkt.HasPrevAct {
		t.Errorf("Module 35 请求必须标记 HasPrevAct 以维持动作链")
	}
	if got := binary.BigEndian.Uint16(pkt.Payload[:2]); got != 13 {
		t.Errorf("期望 town_id=13, 实际 %d", got)
	}
}

// TestHeroMissionNotify_Finish 验证扫荡终态推送解析。
func TestHeroMissionNotify_Finish(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.HeroMissionNotifyFinish)
	w.WriteUint16(0)
	w.WriteUint16(0)
	w.WriteInt16(8)
	n, err := protocol.ParseHeroMissionNotify(w.Bytes())
	if err != nil {
		t.Fatalf("ParseHeroMissionNotify 失败: %v", err)
	}
	if n.Type != protocol.HeroMissionNotifyFinish || n.EmptyBagNumber != 8 {
		t.Errorf("推送解析不一致: %+v", *n)
	}
}

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
