package protocol_test

import (
	"bytes"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 以下 Golden 数据均来自 /tmp/faxiang3.pcapng 的 518 回包（已解压、已剥离 ActionID）。

// 518/11: level=3, exp=1604, award_list=[], cur_award=14, refine_finish_time=1795163235
const goldenFaxiangFoInfo = "000000030000064400000000000e6b000463"

// 518/13: result=0, 3 个候选奖励 id=[5,15,14]
const goldenFaxiangPray = "000003000000050000000f0000000e"

// 518/15: result=0, refine_finish_time=1795339503
const goldenFaxiangChoose = "006b02b4ef"

// 518/12 挑战前: level=1, exp=0, next=0, stunt=282, supernatural=97, pattern=31
const goldenFaxiangMoInfoBefore = "0000000100000000000000000000011a000000610000001f"

// 518/12 挑战后: level=1, exp=28, next=1795619451, stunt=282, supernatural=180, pattern=42
const goldenFaxiangMoInfoAfter = "000000010000001c6b06fa7b0000011a000000b40000002a"

func TestParseFaxiangExtraFoInfo_Golden(t *testing.T) {
	info, err := protocol.ParseFaxiangExtraFoInfo(mustHex(t, goldenFaxiangFoInfo))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.Level != 3 || info.CurExp != 1604 {
		t.Fatalf("level/exp = %d/%d, want 3/1604", info.Level, info.CurExp)
	}
	if len(info.AwardList) != 0 {
		t.Fatalf("award_list = %v, want empty", info.AwardList)
	}
	if info.CurPrayAwardID != 14 {
		t.Fatalf("cur_award_id = %d, want 14", info.CurPrayAwardID)
	}
	if info.RefineFinishTime != 1795163235 {
		t.Fatalf("refine_finish_time = %d, want 1795163235", info.RefineFinishTime)
	}
}

func TestParseFaxiangExtraFoInfo_WithPendingAwards(t *testing.T) {
	w := protocol.NewWriter().
		WriteInt32(2).WriteInt32(940).
		WriteUint16(2).WriteInt32(5).WriteInt32(15).
		WriteInt32(0).WriteInt32(0)
	info, err := protocol.ParseFaxiangExtraFoInfo(w.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(info.AwardList) != 2 || info.AwardList[0] != 5 || info.AwardList[1] != 15 {
		t.Fatalf("award_list = %v, want [5 15]", info.AwardList)
	}
	if info.CurPrayAwardID != 0 {
		t.Fatalf("cur_award_id = %d, want 0", info.CurPrayAwardID)
	}
}

func TestParseFaxiangExtraPrayResult_Golden(t *testing.T) {
	opts, err := protocol.ParseFaxiangExtraPrayResult(mustHex(t, goldenFaxiangPray))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Result != protocol.FaxiangSuccess {
		t.Fatalf("result = %d, want success", opts.Result)
	}
	if len(opts.AwardIDs) != 3 || opts.AwardIDs[0] != 5 || opts.AwardIDs[1] != 15 || opts.AwardIDs[2] != 14 {
		t.Fatalf("award_ids = %v, want [5 15 14]", opts.AwardIDs)
	}
}

func TestParseFaxiangExtraPrayResult_Failure(t *testing.T) {
	opts, err := protocol.ParseFaxiangExtraPrayResult([]byte{protocol.FaxiangCanNotPray})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if opts.Result != protocol.FaxiangCanNotPray || len(opts.AwardIDs) != 0 {
		t.Fatalf("opts = %+v, want result=24 no awards", opts)
	}
}

func TestParseFaxiangChoosePrayAwardResult_Golden(t *testing.T) {
	res, err := protocol.ParseFaxiangChoosePrayAwardResult(mustHex(t, goldenFaxiangChoose))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.FaxiangSuccess || res.RefineFinishTime != 1795339503 {
		t.Fatalf("res = %+v, want success finish=1795339503", res)
	}
}

func TestParseFaxiangExtraMoInfo_Golden(t *testing.T) {
	before, err := protocol.ParseFaxiangExtraMoInfo(mustHex(t, goldenFaxiangMoInfoBefore))
	if err != nil {
		t.Fatalf("parse before: %v", err)
	}
	if before.Level != 1 || before.CurExp != 0 || before.NextCanFightTime != 0 ||
		before.RandomStuntID != 282 || before.RandomSupernaturalID != 97 || before.RandomSpecialPattern != 31 {
		t.Fatalf("before = %+v", before)
	}

	after, err := protocol.ParseFaxiangExtraMoInfo(mustHex(t, goldenFaxiangMoInfoAfter))
	if err != nil {
		t.Fatalf("parse after: %v", err)
	}
	if after.CurExp != 28 || after.NextCanFightTime != 1795619451 ||
		after.RandomSupernaturalID != 180 || after.RandomSpecialPattern != 42 {
		t.Fatalf("after = %+v", after)
	}
}

func TestParseFaxiangExtraMoChallengeResult(t *testing.T) {
	w := protocol.NewWriter().WriteUint8(protocol.FaxiangSuccess).WriteInt32(28)
	// 追加战报前缀，确认解析只消费 kill_num 而不受后续大包影响。
	w.WriteBytes([]byte{0, 1, 3, 0, 1, 0xA0, 0xEC})
	res, err := protocol.ParseFaxiangExtraMoChallengeResult(w.Bytes())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Result != protocol.FaxiangSuccess || res.KillNum != 28 {
		t.Fatalf("res = %+v, want success kill=28", res)
	}

	fail, err := protocol.ParseFaxiangExtraMoChallengeResult([]byte{protocol.FaxiangHasChallenge})
	if err != nil {
		t.Fatalf("parse failure: %v", err)
	}
	if fail.Result != protocol.FaxiangHasChallenge || fail.KillNum != 0 {
		t.Fatalf("fail = %+v, want result=22", fail)
	}
}

func TestBuildFaxiangPackets_PrevAct(t *testing.T) {
	cases := []struct {
		name   string
		pkt    *protocol.Packet
		action uint32
		body   []byte // 不含末尾 4 字节 PrevAct 占位
	}{
		{"panel", protocol.BuildFaxiangGetPanelInfoPacket(), protocol.ActionIDFaxiangGetPanelInfo, nil},
		{"fo_info", protocol.BuildFaxiangExtraFoInfoPacket(), protocol.ActionIDFaxiangExtraFoInfo, nil},
		{"mo_info", protocol.BuildFaxiangExtraMoInfoPacket(), protocol.ActionIDFaxiangExtraMoInfo, nil},
		{"pray", protocol.BuildFaxiangExtraPrayPacket(), protocol.ActionIDFaxiangExtraPray, nil},
		{"choose", protocol.BuildFaxiangExtraChoosePrayAwardPacket(14), protocol.ActionIDFaxiangExtraChoosePrayAward, []byte{0, 0, 0, 14}},
		{"challenge", protocol.BuildFaxiangExtraMoChallengePacket(), protocol.ActionIDFaxiangExtraMoChallenge, nil},
		{"gain", protocol.BuildFaxiangGainPrayAwardPacket(), protocol.ActionIDFaxiangGainPrayAward, nil},
	}
	for _, c := range cases {
		if c.pkt.ActionID != c.action {
			t.Fatalf("%s action = 0x%08X, want 0x%08X", c.name, c.pkt.ActionID, c.action)
		}
		if !c.pkt.HasPrevAct {
			t.Fatalf("%s 缺少 PrevAct 标记", c.name)
		}
		want := append(append([]byte{}, c.body...), 0, 0, 0, 0)
		if !bytes.Equal(c.pkt.Payload, want) {
			t.Fatalf("%s payload = %x, want %x", c.name, c.pkt.Payload, want)
		}
	}
}

func TestFaxiangResultText_KnownCodes(t *testing.T) {
	if got := protocol.FaxiangResultText(protocol.FaxiangNotEnoughDaoYuan); got != "道缘不足" {
		t.Fatalf("道缘结果文本 = %q", got)
	}
	if got := protocol.FaxiangResultText(protocol.FaxiangHasChallenge); got != "已挑战/冷却中" {
		t.Fatalf("挑战冷却结果文本 = %q", got)
	}
}
