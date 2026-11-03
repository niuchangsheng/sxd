// Package protocol_test 验证仙界竞技场 (Mod 214) 封包构造与回包解析。
//
// @author Ateng
// @since 2026-11-02
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func buildStArenaPanelPayload() []byte {
	w := protocol.NewWriter()
	w.WriteInt32(1888)
	w.WriteInt16(4)
	w.WriteInt32(120)
	w.WriteInt32(1762000000)
	w.WriteInt32(2) // reduce_refresh_time
	w.WriteInt32(7) // proficiency

	w.WriteUint16(2)
	w.WriteInt32(7001)
	w.WriteInt32(1750)
	w.WriteInt32(101)
	w.WriteInt16(88)
	w.WriteInt32(7002)
	w.WriteInt32(1900)
	w.WriteInt32(102)
	w.WriteInt16(95)

	w.WriteUint16(1)
	w.WriteInt32(9001)
	w.WriteUint8(0)
	w.WriteInt32(7001)
	w.WriteString("飞羽")
	w.WriteInt32(7002)
	w.WriteString("剑宗")
	w.WriteInt32(7001)
	w.WriteInt32(30)
	w.WriteString("v1")
	w.WriteInt32(1762000100)
	w.WriteUint8(0)

	w.WriteUint16(1)
	w.WriteInt32(7001)
	w.WriteString("飞羽")
	w.WriteString("s1")
	w.WriteString("s1_仙界一区")
	w.WriteInt32(1888)
	w.WriteInt32(520)
	return w.Bytes()
}

func TestParseStArenaPanelResult(t *testing.T) {
	panel, err := protocol.ParseStArenaPanelResult(buildStArenaPanelPayload())
	if err != nil {
		t.Fatalf("解析仙界竞技场面板失败: %v", err)
	}
	if panel.Integral != 1888 || panel.ReduceChallengeCount != 4 {
		t.Errorf("面板基础字段不匹配: %+v", panel)
	}
	if panel.AwardFeats != 120 || panel.AwardGetTime != 1762000000 ||
		panel.ReduceRefreshTime != 2 || panel.Proficiency != 7 {
		t.Errorf("面板奖励/刷新/熟练度字段不匹配: %+v", panel)
	}
	if len(panel.ChallengeList) != 2 {
		t.Fatalf("对手数量不匹配: %d", len(panel.ChallengeList))
	}
	if got := panel.ChallengeList[0]; got.StPlayerID != 7001 || got.Integral != 1750 || got.RoleID != 101 || got.Level != 88 {
		t.Errorf("首个对手字段不匹配: %+v", got)
	}
	if len(panel.ReportList) != 1 {
		t.Fatalf("战报数量不匹配: %d", len(panel.ReportList))
	}
	report := panel.ReportList[0]
	if report.WarReportID != 9001 || report.WinID != 7001 || report.AttackName != "飞羽" || report.DefenseName != "剑宗" {
		t.Errorf("战报字段不匹配: %+v", report)
	}
	if !report.IsWin(7001) || report.IsWin(7002) {
		t.Error("战报胜负判定错误")
	}
	if len(panel.IntegralRankList) != 1 {
		t.Fatalf("排行榜数量不匹配: %d", len(panel.IntegralRankList))
	}
	entry := panel.IntegralRankList[0]
	if entry.StPlayerID != 7001 || entry.NickName != "飞羽" || entry.StageName != "s1" ||
		entry.ServerName != "s1_仙界一区" || entry.Integral != 1888 || entry.Feats != 520 {
		t.Errorf("排行榜字段不匹配: %+v", entry)
	}
}

func TestParseStArenaPanelResult_Truncated(t *testing.T) {
	payload := buildStArenaPanelPayload()
	if _, err := protocol.ParseStArenaPanelResult(payload[:len(payload)-1]); err == nil {
		t.Fatal("截断载荷应返回解析错误")
	}
}

func TestParseStArenaRefreshResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StArenaResultSuccess)
	w.WriteUint16(1)
	w.WriteInt32(8001)
	w.WriteInt32(1600)
	w.WriteInt32(103)
	w.WriteInt16(70)
	w.WriteInt32(1)

	result, err := protocol.ParseStArenaRefreshResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析刷新对手回包失败: %v", err)
	}
	if result.Result != protocol.StArenaResultSuccess || result.ReduceRefreshTime != 1 {
		t.Errorf("刷新结果字段不匹配: %+v", result)
	}
	if len(result.ChallengeList) != 1 || result.ChallengeList[0].StPlayerID != 8001 {
		t.Errorf("刷新对手列表不匹配: %+v", result.ChallengeList)
	}
}

func TestBuildAndParseStArenaChallenge(t *testing.T) {
	pkt, err := protocol.BuildStArenaChallengePacket(protocol.StArenaChallengeRequest{
		StPlayerID: 7001,
		Integral:   1750,
	})
	if err != nil {
		t.Fatalf("构造挑战封包失败: %v", err)
	}
	if pkt.ActionID != protocol.ActionIDStArenaChallenge {
		t.Errorf("挑战 ActionID 不匹配: 0x%08X", pkt.ActionID)
	}
	// 服务端对 challenge 要求严格 8 字节；多出的 PrevAct 尾字节会被静默丢弃。
	if len(pkt.Payload) != 8 {
		t.Errorf("挑战载荷应为严格 8 字节, 实际 %d", len(pkt.Payload))
	}
	if pkt.HasPrevAct {
		t.Error("挑战封包不得追加 PrevAct 尾部")
	}
	req, err := protocol.ParseStArenaChallengeRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("解析挑战请求失败: %v", err)
	}
	if req.StPlayerID != 7001 || req.Integral != 1750 {
		t.Errorf("挑战请求字段不匹配: %+v", req)
	}
}

func TestParseStArenaChallengeResult(t *testing.T) {
	result, err := protocol.ParseStArenaChallengeResult([]byte{protocol.StArenaResultSuccess, 0xFF, 0xFF})
	if err != nil {
		t.Fatalf("解析挑战结果失败: %v", err)
	}
	if !result.Success() {
		t.Errorf("期望挑战成功, 实际 result=%d", result.Result)
	}
	if _, err := protocol.ParseStArenaChallengeResult(nil); err == nil {
		t.Fatal("空载荷应返回解析错误")
	}
}

// TestStArenaResultCodes 锁定官方 Mod_StArena_Base 的结果码数值。
//
// 13 之后的码位连续自增，漏掉 FUNC_END 会让 CD_TIME 之后的全部失败语义整体错位。
func TestStArenaResultCodes(t *testing.T) {
	cases := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"SUCCESS", protocol.StArenaResultSuccess, 13},
		{"CD_TIME", protocol.StArenaResultCDTime, 14},
		{"FUNC_END", protocol.StArenaResultFuncEnd, 15},
		{"FAIL", protocol.StArenaResultFail, 16},
		{"NOT_ENOUGH_CHALLENGE_TIMES", protocol.StArenaResultNotEnoughChallengeTimes, 17},
		{"NOT_IN_LIST", protocol.StArenaResultNotInList, 18},
		{"ITEM_NOT_EXIST", protocol.StArenaResultItemNotExist, 19},
		{"NOT_ENOUGH_EXPLOIT", protocol.StArenaResultNotEnoughExploit, 20},
		{"NOT_ENOUGH_NUM", protocol.StArenaResultNotEnoughNum, 21},
		{"NOT_ENOUGH_FUN", protocol.StArenaResultNotEnoughFun, 22},
		{"PACK_FULL", protocol.StArenaResultPackFull, 23},
		{"REBET", protocol.StArenaResultRebet, 24},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("结果码 %s 不匹配: got=%d want=%d", c.name, c.got, c.want)
		}
	}
}

func TestStArenaActionIDs(t *testing.T) {
	cases := []struct {
		got    uint32
		module uint16
		action uint16
	}{
		{protocol.ActionIDStArenaOpen, 214, 0},
		{protocol.ActionIDStArenaRefreshPlayerList, 214, 1},
		{protocol.ActionIDStArenaChallenge, 214, 2},
		{protocol.ActionIDStArenaClose, 214, 4},
	}
	for _, c := range cases {
		m, a := protocol.SplitActionID(c.got)
		if m != c.module || a != c.action {
			t.Errorf("ActionID 0x%08X 拆分错误: module=%d action=%d", c.got, m, a)
		}
	}
}
