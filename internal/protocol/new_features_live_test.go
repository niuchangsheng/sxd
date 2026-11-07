package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 以下 hex 均来自 s0 线上只读面板探测 (2026-11-06)。

func TestParseSpaceFindOpenResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("00060000000000050000002800000000000000009600")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	info, err := protocol.ParseSpaceFindOpenResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.Count != 6 || info.Golden != 5 || info.CanBuyCount != 40 {
		t.Fatalf("unexpected space find panel: %+v", info)
	}
	if info.CallGoldenIngot != 150 || info.RemainCatchCount != 0 {
		t.Fatalf("unexpected golden/catch fields: %+v", info)
	}
}

func TestParseMysticalTreasurePanelResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("000012e300000541000000000000000300000001000000000000000000000000000400000010000000080000000400000002000000140000000900000005000000060000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	p, err := protocol.ParseMysticalTreasurePanelResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Activity != 4835 || p.CostIngot != 1345 {
		t.Fatalf("unexpected activity/cost: %+v", p)
	}
	if p.Mission1Num != 3 || p.Mission2Num != 1 {
		t.Fatalf("unexpected mission nums: %+v", p)
	}
	if p.DigNum != 0 {
		t.Fatalf("expected dig_num=0, got %d", p.DigNum)
	}
	if len(p.OpenGrids) != 4 {
		t.Fatalf("expected 4 open grids, got %+v", p.OpenGrids)
	}
	if p.OpenGrids[0] != (protocol.MysticalTreasureGrid{GridID: 16, AwardID: 8}) {
		t.Fatalf("unexpected first grid: %+v", p.OpenGrids[0])
	}
	if len(p.BigAwardIDs) != 0 {
		t.Fatalf("expected no big awards, got %+v", p.BigAwardIDs)
	}
}

func TestParseNpcFriendshipInfoResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("000a0000000d0000042e010000000000000000000000000000000000003e00000000000000080000042e010000000000000000000000000000000000011e000000000000000e0000042e01000000000000000000000000000000000001d600000735000000090000042e010000000000000000000000000000000000019c000000000000007b0000042e01000000000000000000000000000000000000b1000000000000007d0000042e01000000000000000000000000000000000000c1000000fd000000150000042e01000000000000000000000000000000000000b000000000000000600000042e010000000000000000000000000000000000022300000000000000260000042e01000000000000000000000000000000000000f700000735000000040000042e01000000000000000000000000000000000001c3000000000000000100000000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	snap, err := protocol.ParseNpcFriendshipInfoResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(snap.Npcs) != 10 {
		t.Fatalf("expected 10 npcs, got %d", len(snap.Npcs))
	}
	first := snap.Npcs[0]
	if first.NpcID != 13 || first.CurExp != 1070 || first.IsFriend != 1 {
		t.Fatalf("unexpected first npc: %+v", first)
	}
	if first.TownNpcID != 62 {
		t.Fatalf("unexpected first npc town: %+v", first)
	}
	if snap.FriendCount != 1 || snap.MissionCount != 0 {
		t.Fatalf("unexpected counters: %+v", snap)
	}
}

func TestNpcFriendshipGivePresentPacketLayout(t *testing.T) {
	pkt := protocol.BuildNpcFriendshipGivePresentPacket(protocol.NpcFriendshipPresentSouvenir, 13)
	if pkt.ActionID != protocol.ActionIDNpcFriendshipGivePresent {
		t.Fatalf("unexpected action: 0x%08X", pkt.ActionID)
	}
	// 官方客户端顺序: present_id=1, npc_id=13。
	want := []byte{0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x0d}
	if string(pkt.Payload) != string(want) {
		t.Fatalf("payload mismatch: got %x want %x", pkt.Payload, want)
	}
}

func TestParseNpcFriendshipFailureCodes(t *testing.T) {
	for _, code := range []uint8{
		protocol.NpcFriendshipResultCountLimit,
		protocol.NpcFriendshipResultNoEnough,
		protocol.NpcFriendshipResultExpMax,
		protocol.NpcFriendshipResultMakeFriendCountLimit,
		protocol.NpcFriendshipResultPackFull,
	} {
		payload := protocol.BuildNpcFriendshipGiveResultPacket(
			protocol.NpcFriendshipGiveResult{Result: code}).Payload
		got, err := protocol.ParseNpcFriendshipGiveResult(payload)
		if err != nil {
			t.Fatalf("code %d: %v", code, err)
		}
		if got.Result != code {
			t.Fatalf("code %d roundtrip got %+v", code, got)
		}
	}
}

func TestCollectionBookletsPopularityAwards(t *testing.T) {
	info := protocol.CollectionBookletsPopularityInfo{
		Popularity: 160,
		ClaimedIDs: []int32{1, 2},
	}
	payload := protocol.BuildCollectionBookletsPopularityInfoResultPacket(info).Payload
	got, err := protocol.ParseCollectionBookletsPopularityInfoResult(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Popularity != 160 || len(got.ClaimedIDs) != 2 {
		t.Fatalf("unexpected popularity info: %+v", got)
	}

	// 声望 10/50/100/150 四档可达，其中 1、2 已领取，应只领取 3、4。
	var eligible []int32
	claimed := map[int32]bool{1: true, 2: true}
	for _, award := range protocol.CollectionBookletsPopularityThresholds {
		if !claimed[award.ID] && got.Popularity >= award.Popularity {
			eligible = append(eligible, award.ID)
		}
	}
	if len(eligible) != 2 || eligible[0] != 3 || eligible[1] != 4 {
		t.Fatalf("unexpected eligible awards: %v", eligible)
	}
}

func TestParseCollectionBookletsApproveFailure(t *testing.T) {
	for _, code := range []uint8{
		protocol.CollectionBookletsResultNotEnoughApproveTimes,
		protocol.CollectionBookletsResultItemPackFull,
		protocol.CollectionBookletsResultNoOpenVisit,
	} {
		res, err := protocol.ParseCollectionBookletsApproveResult(
			protocol.BuildCollectionBookletsApproveResultPacket(
				protocol.CollectionBookletsApproveResult{Result: code}).Payload)
		if err != nil {
			t.Fatalf("code %d: %v", code, err)
		}
		if res.Result != code {
			t.Fatalf("code %d roundtrip got %+v", code, res)
		}
	}
}

func TestParseEudemonsPanelResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("000000000100000000000300000003000000000000020000000000000101000000030000000000000001000000000000000200000000000000030000000c0000000c0000012c00000007000001f400000009000000c8000000060000012c00000001000005780000000a0000019000000008000001f40000000b0000019000000003000001f4000000040000012c00000005000002bc00000002000002bc00000000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	p, err := protocol.ParseEudemonsPanelResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Result != protocol.EudemonsResultSuccess || p.SkinID != 1 || p.StoneMaxLevel != 0 {
		t.Fatalf("unexpected eudemons panel: %+v", p)
	}
	if len(p.Meridians) != 3 {
		t.Fatalf("expected 3 meridians, got %+v", p.Meridians)
	}
	if p.Meridians[0].ID != 3 || p.Meridians[0].IsActive != 0 {
		t.Fatalf("unexpected meridian[0]: %+v", p.Meridians[0])
	}
	if p.Meridians[2].ID != 1 || p.Meridians[2].IsActive != 1 {
		t.Fatalf("unexpected meridian[2]: %+v", p.Meridians[2])
	}
	if len(p.BeastSkill) != 3 || len(p.GemExpList) != 12 || p.TotalStoneStep != 0 {
		t.Fatalf("unexpected skill/gem/total: %+v", p)
	}
}

func TestParseDestinySwordPanelResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("0007001291110000000326000ce59ca3e5ae87e69687e68b93000000ab000000000fccaf000000025e000ce9ad94e69e97e69c88e5a6820000012c010000000bba29000000023b0009e4bd9be78eb2e78f910000012c02000000119e7100000002c50009e9ad94e6b289e9a6990000012c01000000107820000000027b000ce9ad94e8b5b5e781b5e584bf0000012c01000000037a6701000001ff0006e98181e4b8800000012c01000100000001000000010000000100000000000001ff0000000000000000000f5c430000000250000ce9ad94e69d8ee9808de981a50000012c010000000000010000000000000000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	p, err := protocol.ParseDestinySwordPanelResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Roles) != 7 {
		t.Fatalf("expected 7 roles, got %d", len(p.Roles))
	}
	// 第 6 个角色为主角遁一，装备 sword_id=1 的飞剑。
	mainRole := p.Roles[5]
	if mainRole.RoleName != "遁一" || mainRole.IsMainRole != 1 || len(mainRole.EquipList) != 1 {
		t.Fatalf("unexpected main role: %+v", mainRole)
	}
	if mainRole.EquipList[0].SwordID != 1 || len(mainRole.EquipList[0].Intents) != 0 {
		t.Fatalf("unexpected main sword: %+v", mainRole.EquipList[0])
	}
	if p.NowDeployType != 1 || p.ItemNum != 0 || p.JianyiScrap != 0 {
		t.Fatalf("unexpected resources: %+v", p)
	}
}

func TestParseSaArenaGetInfoResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("000501000000000000000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000000000000300000000000000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000000000000000005")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	info, err := protocol.ParseSaArenaGetInfoResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(info.Seats) != 5 || info.AvailableCount != 5 {
		t.Fatalf("unexpected sa arena info: %+v", info)
	}
	for i, seat := range info.Seats {
		if seat.Seq != uint8(i+1) || seat.SaPlayerID != 0 {
			t.Fatalf("expected empty seat %d, got %+v", i+1, seat)
		}
	}
}

func TestParsePartnerLinkInfoResult_RoundTrip(t *testing.T) {
	info := protocol.PartnerLinkInfo{
		Level: 3,
		LinkSoulList: [][2]int16{
			{1, 2},
		},
		PartnerInfoList: []protocol.PartnerLinkPartner{{
			SoulTypeID: 1, RoleID: 511, Level: 300,
			StuntName: "绝技", StuntInfo: "描述", MaxHealth: 1000,
			Strength: 1, Agile: 2, Intellect: 3, IsEnhance: 1,
		}},
		Links: []protocol.PartnerLinkEntry{{
			LinkTypeID: 1, IsActive: protocol.PartnerLinkNo,
			Souls:     []protocol.PartnerLinkSoul{{SoulTypeID: 1, IsEnhance: 0, NeedSoulNumber: 2}},
			IsAdvance: protocol.PartnerLinkNo,
		}},
	}
	payload := protocol.BuildPartnerLinkInfoResultPacket(info).Payload
	got, err := protocol.ParsePartnerLinkInfoResult(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Level != 3 || len(got.Links) != 1 || got.Links[0].IsActive != protocol.PartnerLinkNo {
		t.Fatalf("unexpected partner link info: %+v", got)
	}
	if len(got.PartnerInfoList) != 1 || got.PartnerInfoList[0].StuntName != "绝技" {
		t.Fatalf("unexpected partner info: %+v", got.PartnerInfoList)
	}
}

func TestParsePartnerLinkActivateFailure(t *testing.T) {
	for _, code := range []uint8{
		protocol.PartnerLinkResultLackOfLinkSoul,
		protocol.PartnerLinkResultFailed,
		protocol.PartnerLinkResultFunctionNotOpen,
		protocol.PartnerLinkResultNotEnoughIngot,
	} {
		res, err := protocol.ParsePartnerLinkActivateResult(
			protocol.BuildPartnerLinkActivateResultPacket(
				protocol.PartnerLinkActivateResult{Result: code}).Payload)
		if err != nil {
			t.Fatalf("code %d: %v", code, err)
		}
		if res.Result != code {
			t.Fatalf("code %d roundtrip got %+v", code, res)
		}
	}
}
func TestParseMasterPracticePanelResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("000000000000000005000300000e7a0001b2b00000095d0000000d00001629000000000006000000010000000000000000000000000000000000020000000100000000000000020000000000000002000000000000000000000000000000000002000000010000000000000002000000000000000300000000000000000000000000000000000200000001000000000000000200000000000000050000000000000000000000000000000000020000000100000000000000020000000000000006000000000000000000000000000000000002000000010000000000000002000000000000000f00000000000000010000000000000000000200000001000000000000000200000000")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	p, err := protocol.ParseMasterPracticePanelResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.Result != protocol.MasterPracticeResultSuccess {
		t.Fatalf("expected success, got %+v", p)
	}
	if p.ArtifactSkillLevel != 5 || p.TodayChallengeTimes != 0 {
		t.Fatalf("unexpected header: %+v", p)
	}
	if len(p.HaveItems) != 3 || p.HaveItems[0] != [2]int32{3706, 111280} {
		t.Fatalf("unexpected items: %+v", p.HaveItems)
	}
	if len(p.Jobs) != 6 {
		t.Fatalf("expected 6 jobs, got %+v", p.Jobs)
	}
	if p.Jobs[5].JobID != 15 || p.Jobs[5].TotalStars != 1 {
		t.Fatalf("unexpected last job: %+v", p.Jobs[5])
	}
}

func TestMasterPracticePacketLayouts(t *testing.T) {
	ch := protocol.BuildMasterPracticeChallengePacket(3, 2, 0)
	if ch.ActionID != protocol.ActionIDMasterPracticeChallenge {
		t.Fatalf("unexpected challenge action: 0x%08X", ch.ActionID)
	}
	if got := hex.EncodeToString(ch.Payload); got != "0000000300000002" {
		t.Fatalf("challenge payload mismatch: %s", got)
	}
	up := protocol.BuildMasterPracticeStepUpPacket(15, 0)
	if up.ActionID != protocol.ActionIDMasterPracticeStepUp {
		t.Fatalf("unexpected step_up action: 0x%08X", up.ActionID)
	}
	if got := hex.EncodeToString(up.Payload); got != "0000000f" {
		t.Fatalf("step_up payload mismatch: %s", got)
	}

	// deploy: [player_role_id Int32, grid_type_id UByte]，grid=0 表示下阵。
	deployUp := protocol.BuildMasterPracticeDeployPacket(768553, 9)
	if deployUp.ActionID != protocol.ActionIDMasterPracticeDeploy {
		t.Fatalf("unexpected deploy action: 0x%08X", deployUp.ActionID)
	}
	if got := hex.EncodeToString(deployUp.Payload); got != "000bba2909" {
		t.Fatalf("deploy up payload mismatch: %s", got)
	}
	deployDown := protocol.BuildMasterPracticeDeployPacket(1035439, 0)
	if got := hex.EncodeToString(deployDown.Payload); got != "000fccaf00" {
		t.Fatalf("deploy down payload mismatch: %s", got)
	}
}

func TestParseMasterPracticeFailureCodes(t *testing.T) {
	for _, code := range []uint8{
		protocol.MasterPracticeResultNoChallengeTimes,
		protocol.MasterPracticeResultIsChallenged,
		protocol.MasterPracticeResultLessStars,
		protocol.MasterPracticeResultStepUpCD,
	} {
		got, err := protocol.ParseMasterPracticeActionCode(
			protocol.BuildMasterPracticeActionCodePacket(protocol.ActionIDMasterPracticeChallenge, code).Payload)
		if err != nil || got != code {
			t.Fatalf("code %d: got %d err=%v", code, got, err)
		}
	}
}

func TestDailyGiftsProtocols(t *testing.T) {
	result, coin, err := protocol.ParseCampSalaryResult(
		protocol.BuildCampSalaryResultPacket(protocol.PlayerCampSalarySuccess, 12345).Payload)
	if err != nil || result != protocol.PlayerCampSalarySuccess || coin != 12345 {
		t.Fatalf("camp salary: %d %d %v", result, coin, err)
	}
	state, count, err := protocol.ParseSealSoulDayStoneResult(
		protocol.BuildSealSoulDayStoneResultPacket(protocol.SealSoulDayStoneSuccess, 7).Payload)
	if err != nil || state != protocol.SealSoulDayStoneSuccess || count != 7 {
		t.Fatalf("day stone: %d %d %v", state, count, err)
	}
	info, err := protocol.ParseClientGiftInfoResult(protocol.BuildClientGiftInfoResultPacket(
		protocol.ClientGiftInfo{
			Status:      2,
			Awards:      []protocol.DailyGiftItem{{ItemID: 3345, ItemNum: 200}},
			MonthStatus: 2,
			MonthAwards: []protocol.DailyGiftItem{{ItemID: 3418, ItemNum: 100}},
		}).Payload)
	if err != nil || info.Status != 2 || len(info.Awards) != 1 || info.MonthAwards[0].ItemID != 3418 {
		t.Fatalf("client gift info: %+v %v", info, err)
	}
	if got, err := protocol.ParseClientGiftReceiveResult(
		protocol.BuildClientGiftReceiveResultPacket(protocol.PlayerClientGiftBagFull).Payload); err != nil || got != protocol.PlayerClientGiftBagFull {
		t.Fatalf("client gift receive: %d %v", got, err)
	}
}

func TestAssistantOneKeyResult(t *testing.T) {
	// 线上 s0 实测 type=1 回包解析结果 (6 件奖励)。
	awards := []protocol.AssistantAward{
		{ItemID: 1748, ItemNum: 350000},
		{ItemID: 3981, ItemNum: 30},
	}
	payload := protocol.BuildAssistantOneKeyResultPacket(protocol.AssistantResultSuccess, awards).Payload
	result, got, err := protocol.ParseAssistantOneKeyResult(payload)
	if err != nil || result != protocol.AssistantResultSuccess || len(got) != 2 {
		t.Fatalf("assistant: %d %v %v", result, got, err)
	}
	for _, code := range []uint8{
		protocol.AssistantResultNotEnoughVIP,
		protocol.AssistantResultNotEnoughNum,
		protocol.AssistantResultPackFull,
		protocol.AssistantResultHaveDone,
		protocol.AssistantResultFailed,
	} {
		result, got, err := protocol.ParseAssistantOneKeyResult(
			protocol.BuildAssistantOneKeyResultPacket(code, nil).Payload)
		if err != nil || result != code || len(got) != 0 {
			t.Fatalf("code %d: %d %v %v", code, result, got, err)
		}
	}
}

func TestGodWelfareProtocols(t *testing.T) {
	info, err := protocol.ParseGodWelfareInfoResult(protocol.BuildGodWelfareInfoResultPacket(protocol.GodWelfareInfo{
		Level: 260, IsGeted: 1, StartTime: "a", EndTime: "b", RemainSeconds: 300, Point: 0, MaxPoint: 20,
	}).Payload)
	if err != nil || info.Level != 260 || info.IsGeted != 1 || info.MaxPoint != 20 {
		t.Fatalf("info: %+v %v", info, err)
	}
	awards, err := protocol.ParseGodWelfareLevelAwardInfoResult(protocol.BuildGodWelfareLevelAwardInfoResultPacket(
		[]protocol.GodWelfareLevelAward{{Level: 90, NeedValue: 1739, AwardValue: 2000, IsGet: 0}}).Payload)
	if err != nil || len(awards) != 1 || awards[0].Level != 90 {
		t.Fatalf("awards: %+v %v", awards, err)
	}
	for _, code := range []uint8{
		protocol.GodWelfareResultSuccess,
		protocol.GodWelfareResultHadGet,
		protocol.GodWelfareResultLevelLimit,
		protocol.GodWelfareResultItemPackFull,
		protocol.GodWelfareResultFailure,
	} {
		got, err := protocol.ParseGodWelfareGetAwardResult(protocol.BuildGodWelfareGetAwardResultPacket(code).Payload)
		if err != nil || got != code {
			t.Fatalf("code %d: %d %v", code, got, err)
		}
	}
}

func TestSaintAreaProtocols_LiveShapes(t *testing.T) {
	// 主服 get_login_info 线上实测形态。
	cred, err := protocol.ParseSaintAreaCredentials(func() []byte {
		w := protocol.NewWriter()
		w.WriteString("9x050.sxdweb.xd.com")
		w.WriteUint16(8220)
		w.WriteString("s0")
		w.WriteInt32(1786760000)
		w.WriteString("9164d80306122b6041e4ff352511c5ea")
		w.WriteString("saint_area_s0")
		w.WriteInt32(0)
		return w.Bytes()
	}())
	if err != nil || cred.Host != "9x050.sxdweb.xd.com" || cred.Port != 8220 || cred.ServerName != "s0" {
		t.Fatalf("credentials: %+v %v", cred, err)
	}
	// 主服 get_status 线上实测形态: status=4(CLOSE), 1 个关闭窗口。
	st, err := protocol.ParseSaintAreaStatusResult([]byte{4, 0, 1, 0x59, 0xce, 0x27, 0xb0, 0x59, 0xce, 0x38, 0x18, 0x06})
	if err != nil || st.Status != protocol.SaintAreaStatusClose || len(st.CloseTimes) != 1 {
		t.Fatalf("status: %+v %v", st, err)
	}
	// 圣域登录回包 (result, player_id, server_time)。
	res, pid, stime, err := protocol.ParseSaintAreaLoginResult([]byte{1, 0, 0, 0, 0, 0x6a, 0xec, 0xd0, 0x34})
	if err != nil || res != protocol.SaintAreaResultFailed || pid != 0 || stime == 0 {
		t.Fatalf("login: %d %d %d %v", res, pid, stime, err)
	}
}

func TestSaPracticeRoomProtocols(t *testing.T) {
	info, err := protocol.ParseSaFreeRoomInfoResult(protocol.BuildSaFreeRoomInfoResultPacket(protocol.SaFreeRoomInfo{
		TotalPage: 1, TotalPlayer: 20, PracticeTime: 3600, Exp: 123456, RobTime: 2, PlayerPage: 1,
		RoomQuality: 100, ExpPlusPercent: 50, NeedIngot: 0, NeedDaoYuan: 0,
	}).Payload)
	if err != nil || info.RoomQuality != 100 || info.Exp != 123456 || info.TotalPlayer != 20 {
		t.Fatalf("free room: %+v %v", info, err)
	}
	if code, err := protocol.ParseSaPracticeRoomActionResult(
		protocol.BuildSaPracticeRoomActionResultPacket(protocol.ActionIDSaPracticeRoomSitDown, protocol.SaPracticeRoomResultNoSeat).Payload); err != nil || code != protocol.SaPracticeRoomResultNoSeat {
		t.Fatalf("sit down: %d %v", code, err)
	}
	code, exp, err := protocol.ParseSaPracticeRoomExpResult(
		protocol.BuildSaPracticeRoomExpResultPacket(protocol.SaPracticeRoomResultSuccess, 98765).Payload)
	if err != nil || code != protocol.SaPracticeRoomResultSuccess || exp != 98765 {
		t.Fatalf("exp: %d %d %v", code, exp, err)
	}
	code, _, err = protocol.ParseSaPracticeRoomExpResult(
		protocol.BuildSaPracticeRoomExpResultPacket(protocol.SaPracticeRoomResultNoExp, 0).Payload)
	if err != nil || code != protocol.SaPracticeRoomResultNoExp {
		t.Fatalf("exp no-exp: %d %v", code, err)
	}
}

func TestParseForbiddenBookExchangeInfoResult_Live(t *testing.T) {
	raw, err := hex.DecodeString("000312000a000100051300320000000514006400000005")
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	list, err := protocol.ParseForbiddenBookExchangeInfoResult(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 entries, got %+v", list)
	}
	if list[0] != (protocol.ForbiddenBookExchangeInfo{ForbiddenType: 18, CostCoin: 10, CostIngot: 1, Count: 5}) {
		t.Fatalf("unexpected entry 0: %+v", list[0])
	}
	if list[1] != (protocol.ForbiddenBookExchangeInfo{ForbiddenType: 19, CostCoin: 50, CostIngot: 0, Count: 5}) {
		t.Fatalf("unexpected entry 1: %+v", list[1])
	}
	if list[2] != (protocol.ForbiddenBookExchangeInfo{ForbiddenType: 20, CostCoin: 100, CostIngot: 0, Count: 5}) {
		t.Fatalf("unexpected entry 2: %+v", list[2])
	}
	// 纯铜钱筛选: 仅 19/20 应进入自动兑换路径，18 号因含元宝被排除。
	var copperOnly []uint8
	for _, e := range list {
		if e.CostIngot == 0 && e.CostCoin > 0 {
			copperOnly = append(copperOnly, e.ForbiddenType)
		}
	}
	if len(copperOnly) != 2 || copperOnly[0] != 19 || copperOnly[1] != 20 {
		t.Fatalf("unexpected copper-only types: %v", copperOnly)
	}
}

func TestSpaceFindGetResultLayout(t *testing.T) {
	// 官方 schema: UByte result, [Int item_id, Short num], Short count, Short can_buy_count。
	got, err := protocol.ParseSpaceFindGetResult(protocol.BuildSpaceFindGetResultPacket(protocol.SpaceFindGetResult{
		Result:      protocol.SpaceFindResultSuccess,
		Goods:       []protocol.SpaceFindGoods{{ItemID: 1825, Num: 3}},
		Count:       5,
		CanBuyCount: 40,
	}).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got.Goods) != 1 || got.Goods[0].ItemID != 1825 || got.Goods[0].Num != 3 {
		t.Fatalf("unexpected goods: %+v", got.Goods)
	}
	if got.Count != 5 || got.CanBuyCount != 40 {
		t.Fatalf("unexpected counts: %+v", got)
	}
}

func TestMasterPracticeDeployInfoRoundTrip(t *testing.T) {
	info := protocol.MasterPracticeDeployInfo{
		Result:      protocol.MasterPracticeResultSuccess,
		DeployLevel: 20,
		Roles: []protocol.MasterPracticeDeployRole{
			{PlayerRoleID: 227943, IsMainRole: 1, RoleID: 511, RoleName: "遁一", RoleLevel: 300, IsDeploy: 1},
			{PlayerRoleID: 1035439, RoleID: 606, RoleName: "魔林月如", RoleLevel: 300, IsDeploy: 1},
		},
		Grids: []protocol.MasterPracticeDeployGrid{
			{GridTypeID: 6, PlayerRoleID: 0},
			{GridTypeID: 9, PlayerRoleID: 1035439},
			{GridTypeID: 1, PlayerRoleID: 227943},
		},
	}
	got, err := protocol.ParseMasterPracticeDeployInfoResult(protocol.BuildMasterPracticeDeployInfoResultPacket(info).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.DeployLevel != 20 || len(got.Roles) != 2 || len(got.Grids) != 3 {
		t.Fatalf("unexpected deploy info: %+v", got)
	}
	if got.Roles[0].RoleID != 511 || got.Roles[0].IsMainRole != 1 || got.Roles[1].RoleID != 606 {
		t.Fatalf("unexpected deploy roles: %+v", got.Roles)
	}
	if got.Grids[1].GridTypeID != 9 || got.Grids[1].PlayerRoleID != 1035439 {
		t.Fatalf("unexpected deploy grids: %+v", got.Grids)
	}
}

func TestDragonballSacrificeProtocol(t *testing.T) {
	res, err := protocol.ParseDragonballSacrificeResult(protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{
		Result:     protocol.DragonballResultSuccess,
		AltarBalls: []protocol.DragonballAltarBall{{PlayerDBID: 1655, DragonballID: 12, Level: 1}},
		PlayFlag:   protocol.DragonballPlay,
		SkillLevel: 50,
		SkillExp:   1234,
	}).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.DragonballResultSuccess || res.PlayFlag != protocol.DragonballPlay || len(res.AltarBalls) != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
	for _, code := range []uint8{
		protocol.DragonballResultTimesUseUp,
		protocol.DragonballResultNoEnoughCoin,
		protocol.DragonballResultPackageFull,
		protocol.DragonballResultFunctionNoOpen,
	} {
		got, err := protocol.ParseDragonballSacrificeResult(
			protocol.BuildDragonballSacrificeResultPacket(protocol.DragonballSacrificeResult{Result: code}).Payload)
		if err != nil || got.Result != code {
			t.Fatalf("code %d: %+v %v", code, got, err)
		}
	}
}
