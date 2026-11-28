package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// /tmp/wuliangjiehai.pcapng 853/2 章节面板（now_level=3, chapter=1）。
const eraInfiniteChapterHex = "000000006e00010000000100000003000000010001000000010000775f000100043c35000273300009e99fa6e4bfaee7a59e0000006e0000000000001bee001673304039783035302e7378647765622e78642e636f6d000100000001142cd0ce000a3230323631303130303100010002e32200027330000431323332000000c90000000f00001bee001673304039783035302e7378647765622e78642e636f6d0001000000011427db04000a323032363130313030310000000000000000000000010000000b000400002943000000000000294400000000000029450000000000002946000000000000000000"

// /tmp/wuliangjiehai.pcapng 853/4 挑战回包（胜利）。
const eraInfiniteFightHex = "0000010000000100000001145cb354000a32303236313130363031000000010000"

func TestParseEraInfiniteChapter(t *testing.T) {
	raw, err := hex.DecodeString(eraInfiniteChapterHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	ch, err := protocol.ParseEraInfiniteChapter(raw)
	if err != nil {
		t.Fatalf("ParseEraInfiniteChapter: %v", err)
	}
	if ch.Result != 0 || ch.NowLevel != 3 {
		t.Fatalf("章节面板不符: %+v", ch)
	}
}

func TestParseEraInfiniteFightResult(t *testing.T) {
	raw, err := hex.DecodeString(eraInfiniteFightHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	res, err := protocol.ParseEraInfiniteFightResult(raw)
	if err != nil {
		t.Fatalf("ParseEraInfiniteFightResult: %v", err)
	}
	if res.Result != 0 || !res.IsWin {
		t.Fatalf("挑战回包不符: %+v", res)
	}
	if len(res.Teams) != 1 || res.Teams[0].Seq != 1 || !res.Teams[0].IsWin ||
		res.Teams[0].ReportID != 0x145cb354 || res.Teams[0].Version != "2026110601" {
		t.Fatalf("逐队结算不符: %+v", res.Teams)
	}
}

// TestParseEraInfiniteFightResult_MultiTeamLoss 验证多队伍逐队结果：
// 第 2 队失败导致整体 is_win=0，且逐队 is_win 可被解析用于诊断。
func TestParseEraInfiniteFightResult_MultiTeamLoss(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(0)     // result
	w.WriteUint16(3)    // count
	for _, team := range []struct {
		seq   int32
		win   int32
		rid   int32
		ver   string
	}{{1, 1, 100, "2026112801"}, {2, 0, 101, "2026112801"}, {3, 1, 102, "2026112801"}} {
		w.WriteInt32(team.seq)
		w.WriteInt32(team.win)
		w.WriteInt32(team.rid)
		w.WriteString(team.ver)
	}
	w.WriteInt32(0) // overall is_win

	res, err := protocol.ParseEraInfiniteFightResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseEraInfiniteFightResult: %v", err)
	}
	if res.IsWin {
		t.Fatalf("整体应失败: %+v", res)
	}
	if len(res.Teams) != 3 || res.Teams[1].IsWin || !res.Teams[0].IsWin || !res.Teams[2].IsWin {
		t.Fatalf("逐队结算不符: %+v", res.Teams)
	}
}

func TestBuildEraInfiniteFightPacket(t *testing.T) {
	pkt := protocol.BuildEraInfiniteFightPacket(3)
	if len(pkt.Payload) != 10 || !pkt.PrevActFromLastSent {
		t.Fatalf("853/4 封包长度不符: %d", len(pkt.Payload))
	}
	// [Int 3][Short 0][PrevAct 占位]
	if pkt.Payload[3] != 3 || pkt.Payload[4] != 0 || pkt.Payload[5] != 0 {
		t.Fatalf("853/4 载荷不符: %x", pkt.Payload)
	}
}
