// Package protocol 提供纪元湮劫协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestEraYanjiePackets(t *testing.T) {
	panel := BuildEraYanjiePanelPacket(0)
	if panel.ActionID != ActionIDEraYanjiePanel || !panel.HasPrevAct {
		t.Fatalf("面板封包不符: %+v", panel)
	}
	if got := hex.EncodeToString(panel.Payload); got != "03550000" {
		t.Fatalf("面板载荷不符: %s", got)
	}

	team := BuildEraYanjieMonsterTeamPacket(3, 1, 0)
	if team.ActionID != ActionIDEraYanjieMonsterTeam {
		t.Fatalf("敌方队伍封包 ActionID 不符: 0x%08X", team.ActionID)
	}
	// Int32 level + Int32 deploy_type + Uint32 PrevAct
	if len(team.Payload) != 12 {
		t.Fatalf("敌方队伍载荷长度不符: %d", len(team.Payload))
	}

	fight := BuildEraYanjieFightPacket(3, 1, 1, 0)
	if fight.ActionID != ActionIDEraYanjieFight {
		t.Fatalf("战斗封包 ActionID 不符: 0x%08X", fight.ActionID)
	}
	want := "00000003000000010000000103550003"
	if got := hex.EncodeToString(fight.Payload); got != want {
		t.Fatalf("战斗载荷不符: got=%s want=%s", got, want)
	}
}

func TestEraYanjiePanelParse(t *testing.T) {
	pkt := BuildEraYanjiePanelResultPacket(EraYanjiePanel{
		EraLevel: 1, MissionLevel: 2, RankPerc: 3, Rank: 4, FightEndTS: 1801345604, EraZone: 1,
	})
	res, err := ParseEraYanjiePanelResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析面板失败: %v", err)
	}
	if res.EraLevel != 1 || res.MissionLevel != 2 || res.FightEndTS != 1801345604 || res.EraZone != 1 {
		t.Fatalf("面板解析不符: %+v", res)
	}
	if _, err := ParseEraYanjiePanelResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
}

func TestEraYanjieFightParse(t *testing.T) {
	// 线上样本: result=0, 1 场胜利, war_report_id=0x144f6f90, version=2026110501, is_win=1
	raw, _ := hex.DecodeString("0000010000000100000001144f6f90000a32303236313130353031000000010000")
	res, err := ParseEraYanjieFightResult(raw)
	if err != nil {
		t.Fatalf("解析战斗回包失败: %v", err)
	}
	if res.Result != EraYanjieResultSuccess || res.IsWin != 1 || len(res.Battles) != 1 {
		t.Fatalf("战斗回包解析不符: %+v", res)
	}
	if res.Battles[0].Seq != 1 || res.Battles[0].IsWin != 1 ||
		res.Battles[0].WarReportID != 0x144f6f90 || res.Battles[0].ReportVersion != "2026110501" {
		t.Fatalf("战报条目解析不符: %+v", res.Battles[0])
	}

	// 失败码: 空列表 + WRONG_ID
	failPkt := BuildEraYanjieFightResultPacket(EraYanjieFightResult{Result: EraYanjieResultWrongID, IsWin: 0})
	fail, err := ParseEraYanjieFightResult(failPkt.Payload)
	if err != nil {
		t.Fatalf("解析失败回包失败: %v", err)
	}
	if fail.Result != EraYanjieResultWrongID || len(fail.Battles) != 0 {
		t.Fatalf("失败回包解析不符: %+v", fail)
	}

	if _, err := ParseEraYanjieFightResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
	if code, err := ParseEraYanjieMonsterTeamResult([]byte{EraYanjieResultSuccess}); err != nil || code != EraYanjieResultSuccess {
		t.Fatalf("敌方队伍结果解析不符: code=%d err=%v", code, err)
	}
}