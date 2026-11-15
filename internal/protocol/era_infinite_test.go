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
}

func TestBuildEraInfiniteFightPacket(t *testing.T) {
	pkt := protocol.BuildEraInfiniteFightPacket(3)
	if len(pkt.Payload) != 10 || !pkt.HasPrevAct {
		t.Fatalf("853/4 封包长度不符: %d", len(pkt.Payload))
	}
	// [Int 3][Short 0][PrevAct 占位]
	if pkt.Payload[3] != 3 || pkt.Payload[4] != 0 || pkt.Payload[5] != 0 {
		t.Fatalf("853/4 载荷不符: %x", pkt.Payload)
	}
}
