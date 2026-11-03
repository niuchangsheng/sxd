package protocol_test

import (
	"bytes"
	"compress/zlib"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestFeaturePackets_JieYuanBoxRule(t *testing.T) {
	// 验证核心规则: “除了传说，其他都开”
	rawBoxes := []protocol.JieYuanBoxItem{
		{ItemID: protocol.ItemJieYuanBoxCommon, Count: 10},    // 3937
		{ItemID: protocol.ItemJieYuanBoxAdvanced, Count: 5},   // 3938
		{ItemID: protocol.ItemJieYuanBoxFree, Count: 2},       // 3939
		{ItemID: protocol.ItemJieYuanBoxRare, Count: 8},       // 3940
		{ItemID: protocol.ItemJieYuanBoxSupreme, Count: 3},    // 3941
		{ItemID: protocol.ItemJieYuanBoxLegendary, Count: 99}, // 3942 传说 (必须剔除)
		{ItemID: protocol.ItemJieYuanBoxCommon, Count: 0},     // 0 个应剔除
	}

	filtered := protocol.FilterOpenableJieYuanBoxes(rawBoxes)

	if len(filtered) != 5 {
		t.Fatalf("期望过滤后剩余 5 种可开宝箱, 实际 %d 种", len(filtered))
	}

	for _, b := range filtered {
		if b.ItemID == protocol.ItemJieYuanBoxLegendary {
			t.Errorf("严重违背规则: 传说结缘宝箱 (3942) 未被剔除！")
		}
		if b.Count <= 0 {
			t.Errorf("数量为0的宝箱未被剔除: ID=%d", b.ItemID)
		}
	}
}

func TestFeaturePackets_BuildProtocols(t *testing.T) {
	// 1. 领取奖励 (Module 2, Action 29)
	pClaim := protocol.BuildClaimAwardPacket(0x055a3413, protocol.ActionIDVipAwardQuery)
	if pClaim.ActionID != protocol.ActionIDItemClaimAward {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDItemClaimAward, pClaim.ActionID)
	}

	// 2. 结缘宝箱开箱 (Module 232, Action 34)
	pOpen := protocol.BuildJieYuanBoxOpenPacket(protocol.ItemJieYuanBoxCommon, 4, protocol.ActionIDJieYuanBoxOpenContext)
	if pOpen.ActionID != protocol.ActionIDJieYuanBoxOpen {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDJieYuanBoxOpen, pOpen.ActionID)
	}

	// 3. 日活跃领奖 (Module 293, Action 2)
	pAssistant := protocol.BuildAssistantClaimRewardPacket(0x00EB000D)
	if pAssistant.ActionID != protocol.ActionIDAssistantClaimReward {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDAssistantClaimReward, pAssistant.ActionID)
	}

	// 4. 奇门遁甲 (Module 486, Action 7)
	pQiMen := protocol.BuildQiMenArrayInfoPacket(0)
	if pQiMen.ActionID != protocol.ActionIDQiMenArrayInfo {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDQiMenArrayInfo, pQiMen.ActionID)
	}

	// 5. 粗铁淬炼 (Module 282, Action 1)
	pRefine := protocol.BuildRefineExecutePacket(
		protocol.RefinePoolQualityCoins,
		protocol.RefinePoolTypeOneKey,
		10,
		protocol.ActionIDRefinePoolPanel,
	)
	if pRefine.ActionID != protocol.ActionIDRefineExecute {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDRefineExecute, pRefine.ActionID)
	}

	// 6. 天神赐福 (Module 52, Action 0)
	pGod := protocol.BuildGodBlessClaimPacket(0)
	if pGod.ActionID != protocol.ActionIDGodBlessClaim {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDGodBlessClaim, pGod.ActionID)
	}

	// 7. 劫镖 (Module 314, Action 12)
	pRob := protocol.BuildCaravanHijackPacket(0)
	if pRob.ActionID != protocol.ActionIDCaravanHijack {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDCaravanHijack, pRob.ActionID)
	}

	// 8. 寻仙之路 (Module 466, Action 3)
	pXunXian := protocol.BuildXunXianChallengePacket(0xBC, 0)
	if pXunXian.ActionID != protocol.ActionIDXunXianChallenge {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDXunXianChallenge, pXunXian.ActionID)
	}

	// 9. 锁妖塔 (Module 701, Action 1 & Module 705, Action 1)
	pDemon := protocol.BuildDemonTowerEnterPacket(3, 31, 0)
	if pDemon.ActionID != protocol.ActionIDDemonTowerEnter {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDDemonTowerEnter, pDemon.ActionID)
	}
	pDemonBattle := protocol.BuildDemonTowerBattlePacket(0)
	if pDemonBattle.ActionID != protocol.ActionIDDemonTowerBattle {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDDemonTowerBattle, pDemonBattle.ActionID)
	}

	// 10. 山河游历 (Module 850, Action 0)
	pTravel := protocol.BuildEraTravelRollDicePacket(1, 0)
	if pTravel.ActionID != protocol.ActionIDEraTravelRollDice {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDEraTravelRollDice, pTravel.ActionID)
	}

	// 11. 灵道初解 (Module 711, Action 0)
	pLing := protocol.BuildLingDaoComprehendPacket(0)
	if pLing.ActionID != protocol.ActionIDLingDaoComprehend {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDLingDaoComprehend, pLing.ActionID)
	}

	// 12. 山海幻兽 (Module 696, Action 2 & Action 4)
	pHuan := protocol.BuildHuanshouInvadeChallengePacket(0)
	if pHuan.ActionID != protocol.ActionIDHuanshouInvadeChallenge {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDHuanshouInvadeChallenge, pHuan.ActionID)
	}
	pHuanNode := protocol.BuildHuanshouInvadeNodePacket(1, 0)
	if pHuanNode.ActionID != protocol.ActionIDHuanshouInvadeSettle {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDHuanshouInvadeSettle, pHuanNode.ActionID)
	}

	// 14. 悟法领域 (Module 658, Action 0)
	pWuFa := protocol.BuildWuFaDrawCardPacket(0)
	if pWuFa.ActionID != protocol.ActionIDWuFaDrawCard {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDWuFaDrawCard, pWuFa.ActionID)
	}

	// 15. 宗师修行 (Module 665, Action 5)
	pMaster := protocol.BuildMasterPracticeBreakthroughPacket(1, 1, 0)
	if pMaster.ActionID != protocol.ActionIDMasterPracticeBreakthrough {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDMasterPracticeBreakthrough, pMaster.ActionID)
	}

	// 16. 天帝宝库 (Module 594, Action 6)
	pTreasure := protocol.BuildTreasureHouseDrawPacket(0)
	if pTreasure.ActionID != protocol.ActionIDTreasureHouseDraw {
		t.Errorf("期望 ActionID 0x%08X, 实际 0x%08X", protocol.ActionIDTreasureHouseDraw, pTreasure.ActionID)
	}
}

func TestFeaturePackets_ParseJieYuanBoxList(t *testing.T) {
	// 构造真实抓包回包字节: Count=2, [3937, 10], [3942, 5]
	w := protocol.NewWriter()
	w.WriteUint16(2)
	w.WriteInt32(3937)
	w.WriteInt32(10)
	w.WriteInt32(3942)
	w.WriteInt32(5)

	items, err := protocol.ParseJieYuanBoxListResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("期望 2 项, 实际 %d 项", len(items))
	}
	if items[0].ItemID != 3937 || items[0].Count != 10 {
		t.Errorf("解析第 1 项异常: %+v", items[0])
	}
}

func TestOnlineGift_Packets(t *testing.T) {
	q := protocol.BuildOnlineGiftCurrentPacket()
	if q.ActionID != protocol.ActionIDPlayerGetCurrentOnlineGift {
		t.Errorf("查询封包期望 ActionID 0x%08X, 实际 0x%08X",
			protocol.ActionIDPlayerGetCurrentOnlineGift, q.ActionID)
	}
	if len(q.Payload) != 0 {
		t.Errorf("查询封包请求体应为空, 实际 %d 字节", len(q.Payload))
	}

	c := protocol.BuildOnlineGiftClaimPacket()
	if c.ActionID != protocol.ActionIDPlayerGetOnlineGift {
		t.Errorf("领取封包期望 ActionID 0x%08X, 实际 0x%08X",
			protocol.ActionIDPlayerGetOnlineGift, c.ActionID)
	}
	if len(c.Payload) != 0 {
		t.Errorf("领取封包请求体应为空, 实际 %d 字节", len(c.Payload))
	}
}

func TestOnlineGift_ParseCurrentResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteInt32(1700000000)
	w.WriteUint16(2)
	w.WriteInt32(1)
	w.WriteInt32(3937)
	w.WriteInt32(10)
	w.WriteInt32(2)
	w.WriteInt32(3940)
	w.WriteInt32(5)

	cur, err := protocol.ParseOnlineGiftCurrentResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析查询回包失败: %v", err)
	}
	if cur.TimeStamp != 1700000000 {
		t.Errorf("time_stamp 期望 1700000000, 实际 %d", cur.TimeStamp)
	}
	if len(cur.Gifts) != 2 {
		t.Fatalf("期望 2 项奖励, 实际 %d 项", len(cur.Gifts))
	}
	if cur.Gifts[1].Type != 2 || cur.Gifts[1].ItemID != 3940 || cur.Gifts[1].Value != 5 {
		t.Errorf("第 2 项奖励解析异常: %+v", cur.Gifts[1])
	}
}

func TestOnlineGift_ParseClaimResult(t *testing.T) {
	// 成功回包: result=97 (ACTION_SUCCESS), 1 项奖励, next_time_stamp=1700000600
	w := protocol.NewWriter()
	w.WriteUint8(protocol.PlayerActionSuccess)
	w.WriteUint16(1)
	w.WriteInt32(1)
	w.WriteInt32(1488)
	w.WriteInt32(3)
	w.WriteInt32(1700000600)

	claim, err := protocol.ParseOnlineGiftClaimResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析领取回包失败: %v", err)
	}
	if claim.Result != protocol.PlayerActionSuccess {
		t.Errorf("result 期望 %d, 实际 %d", protocol.PlayerActionSuccess, claim.Result)
	}
	if len(claim.Gifts) != 1 || claim.Gifts[0].ItemID != 1488 {
		t.Errorf("奖励列表解析异常: %+v", claim.Gifts)
	}
	if claim.NextTimeStamp != 1700000600 {
		t.Errorf("next_time_stamp 期望 1700000600, 实际 %d", claim.NextTimeStamp)
	}

	// 实机 s0/s1 回包: 64 0000 00000000 -> result=100 (ACTION_FAILED), 空列表, next_time_stamp=0
	live, err := protocol.ParseOnlineGiftClaimResult([]byte{0x64, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	if err != nil {
		t.Fatalf("解析实机回包失败: %v", err)
	}
	if live.Result != 100 || len(live.Gifts) != 0 || live.NextTimeStamp != 0 {
		t.Errorf("实机回包解析异常: %+v", live)
	}
}

func TestFeaturePackets_Decompress(t *testing.T) {
	raw := []byte("hello sxd protocol payload")
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(raw)
	zw.Close()

	decomp := protocol.DecompressPayloadIfNeeded(buf.Bytes())
	if !bytes.Equal(decomp, raw) {
		t.Errorf("解压缩不匹配, 期望 %s, 实际 %s", string(raw), string(decomp))
	}
}
