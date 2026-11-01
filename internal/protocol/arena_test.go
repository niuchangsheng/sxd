// Package protocol 提供神仙道客户端与服务器之间的二进制协议序列化、反序列化、大端序编解码与透明 zlib 流解压缩引擎。
//
// @author Ateng
// @since 2026-10-09
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestArenaCodec_BuildAndParse(t *testing.T) {
	// 1. 测试 ArenaGetTimesRequest
	getTimesReq := ArenaGetTimesRequest{PrevAct: 0x00200000}
	pktTimes, err := BuildArenaGetTimesPacket(getTimesReq)
	if err != nil {
		t.Fatalf("BuildArenaGetTimesPacket 失败: %v", err)
	}
	if pktTimes.ActionID != ActionIDArenaGetTimes {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDArenaGetTimes, pktTimes.ActionID)
	}

	parsedTimes, err := ParseArenaGetTimesRequest(pktTimes.Payload)
	if err != nil {
		t.Fatalf("ParseArenaGetTimesRequest 失败: %v", err)
	}
	if parsedTimes.PrevAct != 0x00200000 {
		t.Errorf("PrevAct 不匹配: 期望 0x00200000, 实际 0x%08X", parsedTimes.PrevAct)
	}

	// 2. 测试 ArenaGetTimesResult 对齐真实抓包 Hex
	// 真实抓包: 00000005 (5 次剩余)
	resPkt, err := BuildArenaGetTimesResultPacket(5)
	if err != nil {
		t.Fatalf("BuildArenaGetTimesResultPacket 失败: %v", err)
	}
	if hex.EncodeToString(resPkt.Payload) != "00000005" {
		t.Errorf("剩余次数 Hex 不匹配: %s", hex.EncodeToString(resPkt.Payload))
	}
	parsedRes, err := ParseArenaGetTimesResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaGetTimesResult 失败: %v", err)
	}
	if parsedRes.RemainingTimes != 5 {
		t.Errorf("RemainingTimes 不匹配: 期望 5, 实际 %d", parsedRes.RemainingTimes)
	}

	// 3. 测试 ArenaChallengeRequest 对齐线上实测格式
	// 实测: 服务端只接受 00000005 (TargetRank=5)；追加 PrevAct 会导致请求被静默丢弃
	challengeReq := ArenaChallengeRequest{
		TargetRank: 5,
		PrevAct:    ActionIDArenaOpenPanel,
	}
	pktChallenge, err := BuildArenaChallengePacket(challengeReq)
	if err != nil {
		t.Fatalf("BuildArenaChallengePacket 失败: %v", err)
	}
	if pktChallenge.ActionID != ActionIDArenaChallenge {
		t.Fatalf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", ActionIDArenaChallenge, pktChallenge.ActionID)
	}
	if actualHex := hex.EncodeToString(pktChallenge.Payload); actualHex != "00000005" {
		t.Errorf("挑战载荷 Hex 不匹配: 期望 00000005, 实际 %s", actualHex)
	}

	parsedChallenge, err := ParseArenaChallengeRequest(pktChallenge.Payload)
	if err != nil {
		t.Fatalf("ParseArenaChallengeRequest 失败: %v", err)
	}
	if parsedChallenge.TargetRank != 5 || parsedChallenge.PrevAct != 0 {
		t.Errorf("挑战解析结果不符: %+v", parsedChallenge)
	}

	// 4. 测试 ArenaGetOpponentsResult 编解码
	opponents := []ArenaOpponent{
		{Rank: 5, PlayerID: 1001, RoleID: 1, Name: "初级剑仙", Level: 30, AwardCoin: 40000, AwardFame: 10},
		{Rank: 4, PlayerID: 1002, RoleID: 2, Name: "天罗神将", Level: 40, AwardCoin: 45000, AwardFame: 10},
	}
	oppPkt, err := BuildArenaGetOpponentsResultPacket(opponents)
	if err != nil {
		t.Fatalf("BuildArenaGetOpponentsResultPacket 失败: %v", err)
	}
	parsedOppRes, err := ParseArenaGetOpponentsResult(oppPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaGetOpponentsResult 失败: %v", err)
	}
	if len(parsedOppRes.Opponents) != 2 || parsedOppRes.Opponents[0].Name != "初级剑仙" ||
		parsedOppRes.Opponents[1].PlayerID != 1002 || parsedOppRes.Opponents[1].AwardFame != 10 {
		t.Errorf("对手列表解析不匹配: %+v", parsedOppRes)
	}

	// 5. 测试面板回包编解码
	panel := ArenaPanelResult{
		MyRank:               1642,
		WinCount:             0,
		Fame:                 18146901,
		FameLevel:            42,
		RemainChallengeTimes: 5,
		RemainBuyTimes:       100,
		NextBuyPrice:         12,
		AwardCoin:            35800,
		AwardTime:            1793703128,
		OpenTimer:            1793444228,
		CdTimer:              0,
		Opponents:            opponents,
		WarReports: []ArenaWarReport{
			{
				WarReportID:      340047380,
				IsRead:           0,
				AttackPlayerID:   270360,
				AttackPlayerName: "Zues",
				DefensePlayerID:  106732,
				DefensePlayerName: "遁一",
				WinPlayerID:      270360,
				OldRank:          1442,
				NewRank:          1642,
				Version:          "2026102301",
				ReportTimer:      1793288809,
			},
		},
	}
	panelPkt, err := BuildArenaPanelResultPacket(panel)
	if err != nil {
		t.Fatalf("BuildArenaPanelResultPacket 失败: %v", err)
	}
	if panelPkt.ActionID != ActionIDArenaOpenPanel {
		t.Fatalf("面板 ActionID 不匹配: 0x%08X", panelPkt.ActionID)
	}
	parsedPanel, err := ParseArenaPanelResult(panelPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaPanelResult 失败: %v", err)
	}
	if parsedPanel.MyRank != 1642 || parsedPanel.RemainChallengeTimes != 5 ||
		len(parsedPanel.Opponents) != 2 || len(parsedPanel.WarReports) != 1 {
		t.Fatalf("面板解析不匹配: %+v", parsedPanel)
	}
	rep := parsedPanel.WarReports[0]
	if rep.WinPlayerID != 270360 || rep.OldRank != 1442 || rep.NewRank != 1642 ||
		rep.IsWin(270360) == rep.IsWin(106732) || rep.RankDelta() != -200 {
		t.Errorf("战报解析不匹配: %+v", rep)
	}
	if found := parsedPanel.FindWarReport(340047380); found == nil || found.NewRank != 1642 {
		t.Errorf("FindWarReport 未命中: %+v", found)
	}
	if parsedPanel.FindWarReport(1) != nil {
		t.Errorf("FindWarReport 不应命中不存在的战报")
	}

	// 6. 测试 ArenaChallengeResult 编解码 (仅结算头与战报头)
	chRes := ArenaChallengeResult{
		Result:      ArenaResultSuccess,
		Success:     true,
		RemainTimes: 4,
		CdTimer:     0,
		AwardFame:   90,
		AwardCoin:   900000,
		AwardGun:    0,
		ScrapID:     0,
		ReportID:    340247349,
		DeployType:  3,
		WinPlayerID: 106732,
	}
	chPkt, err := BuildArenaChallengeResultPacket(chRes)
	if err != nil {
		t.Fatalf("BuildArenaChallengeResultPacket 失败: %v", err)
	}
	parsedChRes, err := ParseArenaChallengeResult(chPkt.Payload)
	if err != nil {
		t.Fatalf("ParseArenaChallengeResult 失败: %v", err)
	}
	if !parsedChRes.Success || !parsedChRes.IsWin(106732) || parsedChRes.IsWin(272378) ||
		parsedChRes.ReportID != 340247349 || parsedChRes.AwardCoin != 900000 ||
		parsedChRes.DeployType != 3 || parsedChRes.WinPlayerID != 106732 {
		t.Errorf("挑战结算解析不匹配: %+v", parsedChRes)
	}
}

// TestParseArenaGetOpponentsResult_LiveCapture 锁定 2026-11-01 线上 0x001C0001 真实回包布局。
func TestParseArenaGetOpponentsResult_LiveCapture(t *testing.T) {
	const liveHex = "00050001136700055cdd00000003000f000ce6b1aae885bee697972e7331000003e8000000000001130300058bbf00000005000f000ce88ab1e8be89e585b42e7331000003e8000000000001129f00054d8000000002000f000ce6af9be5a989e890b12e7331000003e8000000000001123b000596f9000000010014000ce7aaa6e88da4e69eab2e7331000003e800000000000111d700057472000000050014000857686974652e7331000003e800000000"
	payload, err := hex.DecodeString(liveHex)
	if err != nil {
		t.Fatalf("解码线上回包失败: %v", err)
	}
	res, err := ParseArenaGetOpponentsResult(payload)
	if err != nil {
		t.Fatalf("解析线上回包失败: %v", err)
	}
	if len(res.Opponents) != 5 {
		t.Fatalf("对手数量不匹配: 期望 5, 实际 %d", len(res.Opponents))
	}
	first := res.Opponents[0]
	if first.Rank != 70503 || first.PlayerID != 351453 || first.RoleID != 3 || first.Name != "汪腾旗.s1" ||
		first.Level != 15 || first.AwardCoin != 1000 || first.AwardFame != 0 {
		t.Errorf("首个对手解析不符: %+v", first)
	}
	last := res.Opponents[4]
	if last.Rank != 70103 || last.Name != "White.s1" || last.Level != 20 {
		t.Errorf("末个对手解析不符: %+v", last)
	}
}

// TestParseArenaPanelResult_LiveCapture 锁定 2026-11-02 线上 0x001C0000 真实面板回包。
// 该回包为挑战获胜并刷新后的面板: myRank=1592, 最新战报 oldRank=1642 -> newRank=1592。
func TestParseArenaPanelResult_LiveCapture(t *testing.T) {
	const liveHex = "0000063800010114e6af002a00040064000c00009f60000000006ae9bcd86ae5c984000000000005000006060003165c000001ff012c0009e9b281e7919fe5af920000b2e800000000000005d40002fbac000000ce012c0008e6a080e5a48fc2b00000c67000000000000005bb00044e140000000501150006e781b5e5878c0000d03400000000000005a20003898d00000200012c0010e4bd8ee8b083e79a84e4b8bbe5aeb0300000d9f80000000000000589000357a70000006300e9000ce5a4aae69893e697a0e5bda20000e3bc0000000000051447c335010001a0ec0006e98181e4b880000427fa0009e5a48fe58d97e7949f0001a0ec0000066a00000638000a323032363130323330316ae765881444b614010004201800045a7565730001a0ec0006e98181e4b88000042018000005a20000066a000a323032363130323330316ae36a691443563a0100042fd60009e6b19fe88bb1e4b9890001a0ec0006e98181e4b88000042fd600000525000005a2000a323032363130323330316ae19c19143f333c010001a0ec0006e98181e4b880000333390009e5878ce8beb0e890bd000333390000052500000525000a323032363130323230316adcd8901413a0ca01000427fa0009e5a48fe58d97e7949f0001a0ec0006e98181e4b8800001a0ec0000052500000525000a323032363039323430316abbb415"
	payload, err := hex.DecodeString(liveHex)
	if err != nil {
		t.Fatalf("解码线上面板回包失败: %v", err)
	}
	res, err := ParseArenaPanelResult(payload)
	if err != nil {
		t.Fatalf("解析线上面板回包失败: %v", err)
	}
	if res.MyRank != 1592 || res.RemainChallengeTimes != 4 || res.RemainBuyTimes != 100 ||
		res.NextBuyPrice != 12 || res.FameLevel != 42 {
		t.Errorf("面板头部解析不符: %+v", res)
	}
	if len(res.Opponents) != 5 || len(res.WarReports) != 5 {
		t.Fatalf("面板列表数量不符: opponents=%d reports=%d", len(res.Opponents), len(res.WarReports))
	}
	opp := res.Opponents[0]
	if opp.Rank != 1542 || opp.PlayerID != 202332 || opp.RoleID != 511 ||
		opp.Level != 300 || opp.Name != "鲁瑟寒" || opp.AwardCoin != 45800 || opp.AwardFame != 0 {
		t.Errorf("面板对手解析不符: %+v", opp)
	}
	if len(res.WarReports) != 5 {
		t.Fatalf("战报数量不匹配: %d", len(res.WarReports))
	}
	rep := res.WarReports[0]
	if rep.WarReportID != 340247349 || rep.AttackPlayerID != 106732 || rep.AttackPlayerName != "遁一" ||
		rep.DefensePlayerID != 272378 || rep.DefensePlayerName != "夏南生" {
		t.Errorf("战报参与方解析不符: %+v", rep)
	}
	// 胜负位与精确名次: 本方 106732 获胜, 1642 -> 1592 (名次上升 50)
	if rep.WinPlayerID != 106732 || !rep.IsWin(106732) || rep.IsWin(272378) {
		t.Errorf("战报胜负位解析不符: %+v", rep)
	}
	if rep.OldRank != 1642 || rep.NewRank != 1592 || rep.RankDelta() != 50 {
		t.Errorf("战报名次解析不符: %+v", rep)
	}
}

// TestParseArenaChallengeResult_LiveCapture 用 2026-11-02 线上 0x001C0002 回包前缀锁定结算头与战报头。
// 原始回包 52945 字节 (完整战报), 胜利方 winPlayerID=106732 即本方。
func TestParseArenaChallengeResult_LiveCapture(t *testing.T) {
	const livePrefixHex = "0000040000000000010000005a000dbba000000000000000001447c3350001030001a0ec"
	payload, err := hex.DecodeString(livePrefixHex)
	if err != nil {
		t.Fatalf("解码线上挑战回包前缀失败: %v", err)
	}
	res, err := ParseArenaChallengeResult(payload)
	if err != nil {
		t.Fatalf("解析线上挑战回包失败: %v", err)
	}
	if !res.Success || res.Result != ArenaResultSuccess || res.RemainTimes != 4 || res.CdTimer != 0 {
		t.Errorf("结算头解析不符: %+v", res)
	}
	if res.AwardFame != 90 || res.AwardCoin != 900000 || res.AwardGun != 0 ||
		res.ScrapID != 0 || res.ReportID != 340247349 {
		t.Errorf("奖励解析不符: %+v", res)
	}
	if res.DeployType != 3 || res.WinPlayerID != 106732 || !res.IsWin(106732) || res.IsWin(272378) {
		t.Errorf("战报头胜负位解析不符: %+v", res)
	}
	if len(res.WarReport) != len(payload) {
		t.Errorf("原始战报载荷长度不匹配: %d", len(res.WarReport))
	}
}