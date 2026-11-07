// Package protocol 提供九空无界协议编解码测试。
//
// @author Ateng
// @since 2026-11-08
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestNineRegionsPanelAndStart(t *testing.T) {
	// 2026-11-08 s1 线上样本: is_first=0, cur_jie=1, cur_level=0, hidden_level=0。
	raw, err := hex.DecodeString("00000000010000000000000000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	panel, err := ParseNineRegionsPanelResult(raw)
	if err != nil {
		t.Fatalf("parse panel: %v", err)
	}
	if panel.IsFirst || panel.CurJie != 1 || panel.CurLevel != 0 || panel.HiddenLevel != 0 {
		t.Fatalf("面板字段不符: %+v", panel)
	}

	start, err := ParseNineRegionsStartResult(BuildNineRegionsStartResultPacket(
		NineRegionsStartResult{Result: NineRegionsResultChallengeSuccess}).Payload)
	if err != nil || start.FirstChallenge || start.Result != NineRegionsResultChallengeSuccess {
		t.Fatalf("开始挑战解析不符: %+v err=%v", start, err)
	}
}

func TestNineRegionsBuilders(t *testing.T) {
	if pkt := BuildNineRegionsStartChallengePacket(1, 1); pkt.ActionID != ActionIDNineRegionsStartChallenge ||
		hex.EncodeToString(pkt.Payload) != "0000000100000001" {
		t.Fatalf("start 封包不符: %+v", pkt)
	}
	if pkt := BuildNineRegionsChooseHelperPacket(77, 1); pkt.ActionID != ActionIDNineRegionsChooseHelper ||
		hex.EncodeToString(pkt.Payload) != "0000004d01" {
		t.Fatalf("choose helper 封包不符: %+v", pkt)
	}
	if pkt := BuildNineRegionsHiddenPacket(7); pkt.ActionID != ActionIDNineRegionsHidden ||
		hex.EncodeToString(pkt.Payload) != "00000007" {
		t.Fatalf("hidden 封包不符: %+v", pkt)
	}
}

func TestNineRegionsTeamInfoAndHelpers(t *testing.T) {
	team, err := ParseNineRegionsTeamInfoResult(BuildNineRegionsTeamInfoResultPacket(NineRegionsTeamInfo{
		LeaderID: 372164,
		Members: []NineRegionsTeamMember{{
			PlayerID: 372164, Name: "曹植始.s1", Sign: "JianLingNanFeiYu",
			Level: 80, RoleID: 3, WarPower: 3, VipLevel: 0,
		}},
	}).Payload)
	if err != nil || team.LeaderID != 372164 || len(team.Members) != 1 || team.Members[0].Name != "曹植始.s1" {
		t.Fatalf("队伍解析不符: %+v err=%v", team, err)
	}

	roles, err := ParseNineRegionsHelperListResult(BuildNineRegionsHelperListResultPacket([]NineRegionsHelperRole{
		{RoleID: 77, Name: "仙猪八戒", Level: 80, JobID: 5, State: NineRegionsResultDeploy},
		{RoleID: 75, Name: "仙少年沉香", Level: 50, JobID: 6, State: NineRegionsResultNoDeploy},
	}).Payload)
	if err != nil || len(roles) != 2 || roles[1].RoleID != 75 || roles[1].State != NineRegionsResultNoDeploy {
		t.Fatalf("助战解析不符: %+v err=%v", roles, err)
	}
}

func TestNineRegionsFightAndCD(t *testing.T) {
	fight, err := ParseNineRegionsFightResult(BuildNineRegionsFightResultPacket(
		NineRegionsResultSuccess, 2).Payload)
	if err != nil || fight.Result != NineRegionsResultSuccess || fight.WarCount != 2 {
		t.Fatalf("挑战解析不符: %+v err=%v", fight, err)
	}
	fail, err := ParseNineRegionsFightResult(BuildNineRegionsFightResultPacket(
		NineRegionsResultChallenged, 0).Payload)
	if err != nil || fail.Result != NineRegionsResultChallenged {
		t.Fatalf("挑战失败解析不符: %+v err=%v", fail, err)
	}
	cd, err := ParseNineRegionsCDResult(BuildNineRegionsCDResultPacket(3).Payload)
	if err != nil || cd != 3 {
		t.Fatalf("CD 解析不符: %d err=%v", cd, err)
	}
	if text := NineRegionsResultText(NineRegionsResultCantChallengeHidden); text != "不可挑战隐藏关卡" {
		t.Fatalf("结果码文本不符: %s", text)
	}
}
