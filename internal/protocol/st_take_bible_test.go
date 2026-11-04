// Package protocol_test 验证仙界跨服西天取经 (Mod 114) 回包解析与封包构造。
//
// @author Ateng
// @since 2026-11-02
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestStTakeBibleCross_ActionIDs(t *testing.T) {
	cases := []struct {
		got  uint32
		want uint32
	}{
		{protocol.ActionIDStTakeBibleCrossOpen, 0x00720000},
		{protocol.ActionIDStTakeBibleCrossClose, 0x00720001},
		{protocol.ActionIDStTakeBibleCrossGetInfo, 0x00720002},
		{protocol.ActionIDStTakeBibleCrossGetPlayer, 0x00720009},
		{protocol.ActionIDStTakeBibleCrossRefresh, 0x00720008},
		{protocol.ActionIDStTakeBibleCrossStart, 0x0072000A},
		{protocol.ActionIDStTakeBibleCrossRob, 0x0072000B},
		{protocol.ActionIDStTakeBibleCrossUpdateData, 0x0072000F},
		{protocol.ActionIDStTakeBibleCrossGetRobCD, 0x00720010},
		{protocol.ActionIDStTakeBibleCrossGetRecentRobPlayer, 0x00720013},
		{protocol.ActionIDStTakeBibleCrossBuyRobTimes, 0x00720016},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("action id mismatch: got 0x%08X want 0x%08X", c.got, c.want)
		}
	}
}

func TestParseStTakeBibleCrossInfoResult(t *testing.T) {
	w := protocol.NewWriter()
	// 2 个使者选项
	w.WriteUint16(2)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionBaiLongMa)
	w.WriteInt8(1)
	w.WriteInt32(1000)
	w.WriteInt16(10)
	w.WriteInt16(20)
	w.WriteInt8(5)
	w.WriteInt32(30)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionTangSeng)
	w.WriteInt8(2)
	w.WriteInt32(5000)
	w.WriteInt16(50)
	w.WriteInt16(60)
	w.WriteInt8(15)
	w.WriteInt32(70)
	// 1 个可保护好友
	w.WriteUint16(1)
	w.WriteInt32(65536)
	w.WriteString("曹植始.s1")
	w.WriteInt8(2)
	w.WriteInt8(3)
	w.WriteInt8(1)
	// 尾部字段
	w.WriteUint8(1)  // takeBibleTimes
	w.WriteInt8(3)   // totalTakeBibleTimes
	w.WriteInt16(20) // refreshPrice
	w.WriteInt8(2)   // takeBibleStatus
	w.WriteUint8(protocol.StTakeBibleCrossProtectionShaWuJing)
	w.WriteString("曹植始.s1")
	w.WriteInt8(1) // bless
	w.WriteInt8(2) // buff

	info, err := protocol.ParseStTakeBibleCrossInfoResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossInfoResult failed: %v", err)
	}
	if len(info.ProtectionList) != 2 || len(info.ProtectPlayerList) != 1 {
		t.Fatalf("unexpected list sizes: %+v", info)
	}
	if info.ProtectionList[1].Protection != protocol.StTakeBibleCrossProtectionTangSeng ||
		info.ProtectionList[1].AwardCoin != 5000 || info.ProtectionList[1].NpcExpe != 70 {
		t.Errorf("unexpected protection record: %+v", info.ProtectionList[1])
	}
	if info.ProtectPlayerList[0].PlayerID != 65536 ||
		info.ProtectPlayerList[0].NickName != "曹植始.s1" ||
		info.ProtectPlayerList[0].RemainProtectTimes != 2 {
		t.Errorf("unexpected protect player: %+v", info.ProtectPlayerList[0])
	}
	if info.TakeBibleTimes != 1 || info.TotalTakeBibleTimes != 3 || info.RefreshPrice != 20 {
		t.Errorf("unexpected tail counters: %+v", info)
	}
	if info.TakeBibleStatus != 2 || info.CanProtection != protocol.StTakeBibleCrossProtectionShaWuJing {
		t.Errorf("unexpected status/protection: %+v", info)
	}
	if info.ProtectPlayer != "曹植始.s1" || info.Bless != 1 || info.Buff != 2 {
		t.Errorf("unexpected tail fields: %+v", info)
	}
}

// TestParseStTakeBibleCrossInfoResult_LivePayload 使用 2026-11-02 实机采集的
// get_take_bible_info 回包 (89 字节) 做逐字节回归，防止字段宽度再次错位。
//
// 该回包证实：每个使者条目 15 字节，npc_expe 为 4 字节 Int 而非 AS3 描述的 Short。
func TestParseStTakeBibleCrossInfoResult_LivePayload(t *testing.T) {
	const liveHex = "0005011400231860007800b4080000171a0219004630c000e101180600000937031e008c618001a4017c0700000079042300af79e0028a0244040000092b0528013ee8a006a405000300000272000000080000000000000000"

	payload, err := hex.DecodeString(liveHex)
	if err != nil {
		t.Fatalf("decode live payload: %v", err)
	}
	info, err := protocol.ParseStTakeBibleCrossInfoResult(payload)
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossInfoResult failed: %v", err)
	}
	if len(info.ProtectionList) != 5 {
		t.Fatalf("expected 5 protection options, got %d", len(info.ProtectionList))
	}
	wantProtections := []uint8{1, 2, 3, 4, 5}
	wantCoins := []int32{2300000, 4600000, 9200000, 11500000, 20900000}
	wantExpe := []int32{5914, 2359, 121, 2347, 626}
	for i, p := range info.ProtectionList {
		if p.Protection != wantProtections[i] || p.AwardCoin != wantCoins[i] || p.NpcExpe != wantExpe[i] {
			t.Errorf("protection[%d] mismatch: %+v", i, p)
		}
	}
	if len(info.ProtectPlayerList) != 0 {
		t.Errorf("expected no protect players, got %+v", info.ProtectPlayerList)
	}
	if info.TakeBibleTimes != 0 || info.TotalTakeBibleTimes != 8 {
		t.Errorf("unexpected take bible times: %d/%d", info.TakeBibleTimes, info.TotalTakeBibleTimes)
	}
	if info.RefreshPrice != 0 || info.TakeBibleStatus != 0 || info.CanProtection != 0 {
		t.Errorf("unexpected state: price=%d status=%d can_protection=%d",
			info.RefreshPrice, info.TakeBibleStatus, info.CanProtection)
	}
	if info.ProtectPlayer != "" || info.Bless != 0 || info.Buff != 0 {
		t.Errorf("unexpected tail fields: %+v", info)
	}
}

func TestParseStTakeBibleCrossStartResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
	w.WriteInt8(4)
	msg, itemID, err := protocol.ParseStTakeBibleCrossStartResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossStartResult failed: %v", err)
	}
	if msg != protocol.StTakeBibleCrossMsgSuccess || itemID != 4 {
		t.Errorf("unexpected start result: msg=%d item=%d", msg, itemID)
	}

	// 失败回包可能只含消息码
	w2 := protocol.NewWriter()
	w2.WriteUint8(protocol.StTakeBibleCrossMsgFullTakeBibleTimes)
	msg, itemID, err = protocol.ParseStTakeBibleCrossStartResult(w2.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossStartResult (msg only) failed: %v", err)
	}
	if msg != protocol.StTakeBibleCrossMsgFullTakeBibleTimes || itemID != 0 {
		t.Errorf("unexpected msg-only result: msg=%d item=%d", msg, itemID)
	}
}

func TestParseStTakeBibleCrossRefreshResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionTangSeng)
	w.WriteInt16(40)
	w.WriteInt8(1)
	msg, protection, nextPrice, err := protocol.ParseStTakeBibleCrossRefreshResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRefreshResult failed: %v", err)
	}
	if msg != protocol.StTakeBibleCrossMsgSuccess || protection != protocol.StTakeBibleCrossProtectionTangSeng || nextPrice != 40 {
		t.Errorf("unexpected refresh result: msg=%d protection=%d next=%d", msg, protection, nextPrice)
	}
}

func TestStTakeBibleCrossProtectionName(t *testing.T) {
	if protocol.StTakeBibleCrossProtectionName(protocol.StTakeBibleCrossProtectionTangSeng) != "唐僧" {
		t.Errorf("unexpected name for TangSeng")
	}
	if protocol.StTakeBibleCrossProtectionName(0) != "未知使者" {
		t.Errorf("unexpected name for unknown protection")
	}
}

func TestBuildStTakeBibleCrossRobPacket(t *testing.T) {
	pkt := protocol.BuildStTakeBibleCrossRobPacket(12345, 2)
	if pkt.ActionID != protocol.ActionIDStTakeBibleCrossRob {
		t.Fatalf("unexpected action id: 0x%08X", pkt.ActionID)
	}
	want := []byte{0x00, 0x00, 0x30, 0x39, 0x02}
	if got := pkt.Payload; string(got) != string(want) {
		t.Errorf("unexpected rob payload: got % X want % X", got, want)
	}

	playerPkt := protocol.BuildStTakeBibleCrossGetPlayerPacket(12345, 180, 1, 2)
	if playerPkt.ActionID != protocol.ActionIDStTakeBibleCrossGetPlayer {
		t.Fatalf("unexpected get player action id: 0x%08X", playerPkt.ActionID)
	}
	wantPlayer := []byte{0x00, 0x00, 0x30, 0x39, 0x00, 0xb4, 0x01, 0x02}
	if got := playerPkt.Payload; string(got) != string(wantPlayer) {
		t.Errorf("unexpected get player payload: got % X want % X", got, wantPlayer)
	}
}

func TestParseStTakeBibleCrossRoadResult(t *testing.T) {
	w := protocol.NewWriter()
	// 2 支持护送队伍
	w.WriteUint16(2)
	w.WriteInt32(1001)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionTangSeng)
	w.WriteInt32(1700000000)
	w.WriteInt32(1700003600)
	w.WriteInt16(80)
	w.WriteInt32(2001)
	w.WriteInt8(1)
	w.WriteInt8(0)
	w.WriteInt32(1002)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionBaiLongMa)
	w.WriteInt32(1700000100)
	w.WriteInt32(1700000400)
	w.WriteInt16(10)
	w.WriteInt32(0)
	w.WriteInt8(2)
	w.WriteInt8(1)
	// 路况尾部字段
	w.WriteInt32(2001) // protectPlayerID
	w.WriteInt16(3)    // canRobTimes
	w.WriteInt8(2)     // canProtectTimes
	w.WriteInt8(1)     // canTakeBibleTimes
	w.WriteInt8(1)     // quicklyTimes
	w.WriteInt8(4)     // bless
	w.WriteUint8(8)    // type
	w.WriteInt32(1700000000)
	// 1 条被劫记录
	w.WriteUint16(1)
	w.WriteInt8(1)
	w.WriteInt16(120)
	w.WriteString("劫匪甲")
	w.WriteString("求败")
	w.WriteString("fengwanyx_s1")
	w.WriteInt16(3001)
	w.WriteInt8(2) // buff

	road, err := protocol.ParseStTakeBibleCrossRoadResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRoadResult failed: %v", err)
	}
	if len(road.Teams) != 2 {
		t.Fatalf("unexpected team count: %d", len(road.Teams))
	}
	if road.Teams[0].PlayerID != 1001 || road.Teams[0].Protection != protocol.StTakeBibleCrossProtectionTangSeng ||
		road.Teams[0].ProtectPlayerID != 2001 || road.Teams[0].SequenceID != 1 {
		t.Errorf("unexpected first team: %+v", road.Teams[0])
	}
	if road.Teams[1].PlayerID != 1002 || road.Teams[1].Distance != 10 || road.Teams[1].IsDeedsSo != 1 {
		t.Errorf("unexpected second team: %+v", road.Teams[1])
	}
	if road.CanRobTimes != 3 || road.CanTakeBibleTimes != 1 || road.Type != 8 || road.Buff != 2 {
		t.Errorf("unexpected road counters: %+v", road)
	}
	if len(road.LastRobs) != 1 || road.LastRobs[0].LastRobNickname != "劫匪甲" ||
		road.LastRobs[0].RemainRecaptureTime != 120 || road.LastRobs[0].RobRoleID != 3001 {
		t.Errorf("unexpected last rob record: %+v", road.LastRobs)
	}
}

func TestParseStTakeBibleCrossPlayerInfoResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteInt32(1001)
	w.WriteString("曹植始")
	w.WriteString("求败")
	w.WriteString("fengwanyx_s1")
	w.WriteInt16(180)
	w.WriteInt8(1) // beRobTimes
	w.WriteInt8(3) // totalCanRobTimes
	w.WriteString("护航者")
	w.WriteInt32(500000)
	w.WriteInt16(120)
	w.WriteInt16(80)
	w.WriteInt32(2000000)
	w.WriteInt16(200)
	w.WriteInt16(100)
	w.WriteInt8(0)   // isRobbed
	w.WriteInt16(0)  // protectTime
	w.WriteInt8(15)  // npcLevel
	w.WriteInt16(30) // robExpe
	w.WriteInt16(60) // awardExpe
	w.WriteInt8(2)   // sequenceID

	info, err := protocol.ParseStTakeBibleCrossPlayerInfoResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossPlayerInfoResult failed: %v", err)
	}
	if info.PlayerID != 1001 || info.NickName != "曹植始" || info.StageName != "求败" ||
		info.ServerName != "fengwanyx_s1" {
		t.Errorf("unexpected identity: %+v", info)
	}
	if info.Level != 180 || info.BeRobTimes != 1 || info.TotalCanRobTimes != 3 ||
		info.RobCoin != 500000 || info.RobFame != 120 || info.RobStExp != 80 {
		t.Errorf("unexpected combat fields: %+v", info)
	}
	if info.IsRobbed != 0 || info.ProtectTime != 0 || info.SequenceID != 2 {
		t.Errorf("unexpected state fields: %+v", info)
	}
}

func TestParseStTakeBibleCrossRobResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
	// stageServer 为带 Uint16 计数的列表，官方客户端取第 2 条作为被劫队伍。
	w.WriteUint16(2)
	w.WriteInt32(999)
	w.WriteString("自己")
	w.WriteString("求败")
	w.WriteString("fengwanyx_s1")
	w.WriteInt32(1001)
	w.WriteString("曹植始")
	w.WriteString("求败")
	w.WriteString("fengwanyx_s1")
	w.WriteInt32(500000)
	w.WriteInt16(120)
	w.WriteInt16(80)
	w.WriteInt32(88001)
	// 战报单元列表：Uint16 单元数，单元前缀 Uint8 deploy_type, Int32 win_player_id。
	w.WriteUint16(1)
	w.WriteUint8(3)
	w.WriteInt32(999)

	result, err := protocol.ParseStTakeBibleCrossRobResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRobResult failed: %v", err)
	}
	if len(result.StageServer) != 2 {
		t.Fatalf("unexpected stage server count: %d", len(result.StageServer))
	}
	if result.Msg != protocol.StTakeBibleCrossMsgSuccess || result.TargetPlayerID != 1001 ||
		result.TargetNickName != "曹植始" || result.RobCoin != 500000 ||
		result.RobFame != 120 || result.RobStExp != 80 || result.WarReportID != 88001 {
		t.Errorf("unexpected rob result: %+v", result)
	}
	if !result.WarParsed || result.DeployType != 3 || result.WinPlayerID != 999 {
		t.Fatalf("unexpected war outcome: %+v", result)
	}
	if !result.IsWin(999) || result.IsWin(1001) {
		t.Errorf("IsWin must follow win_player_id, got %+v", result)
	}

	// 失败回包可能只含结果码。
	w2 := protocol.NewWriter()
	w2.WriteUint8(protocol.StTakeBibleCrossMsgHaveCDTime)
	result, err = protocol.ParseStTakeBibleCrossRobResult(w2.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRobResult (msg only) failed: %v", err)
	}
	if result.Msg != protocol.StTakeBibleCrossMsgHaveCDTime {
		t.Errorf("unexpected msg-only rob result: %+v", result)
	}
}

func TestParseStTakeBibleCrossRobCDResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteInt16(45)
	w.WriteInt16(20)

	cd, err := protocol.ParseStTakeBibleCrossRobCDResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRobCDResult failed: %v", err)
	}
	if cd.CDSeconds != 45 || cd.ClearIngot != 20 {
		t.Errorf("unexpected rob cd: %+v", cd)
	}
}

func TestParseStTakeBibleCrossUpdateDataResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteInt16(3)
	w.WriteUint8(1)

	update, err := protocol.ParseStTakeBibleCrossUpdateDataResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossUpdateDataResult failed: %v", err)
	}
	if update.CanRobTimes != 3 || update.CanTakeBibleTimes != 1 {
		t.Errorf("unexpected update data: %+v", update)
	}
}

func TestParseStTakeBibleCrossRecentRobResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint16(2)
	w.WriteInt32(1001)
	w.WriteInt32(1002)

	players, err := protocol.ParseStTakeBibleCrossRecentRobResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRecentRobResult failed: %v", err)
	}
	if len(players) != 2 || players[0] != 1001 || players[1] != 1002 {
		t.Errorf("unexpected recent rob players: %v", players)
	}
}

func TestStTakeBibleCrossRobMsgName(t *testing.T) {
	cases := map[uint8]string{
		protocol.StTakeBibleCrossMsgSuccess:           "打劫成功",
		protocol.StTakeBibleCrossMsgFullRobTimes:      "今日打劫次数已满",
		protocol.StTakeBibleCrossMsgHaveCDTime:        "打劫冷却中",
		protocol.StTakeBibleCrossMsgInvalidSequenceID: "队伍序列号无效",
		protocol.StTakeBibleCrossMsgBeRobAll:          "目标队伍已被抢光",
		protocol.StTakeBibleCrossMsgRerob:             "已经打劫过该队伍",
	}
	for msg, want := range cases {
		if got := protocol.StTakeBibleCrossRobMsgName(msg); got != want {
			t.Errorf("msg %d: got %q want %q", msg, got, want)
		}
	}
}
