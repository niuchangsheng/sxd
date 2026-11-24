package protocol

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"testing"
	"time"
)

func stUnionGodRecordsFixture(now time.Time) []StUnionGodRecord {
	return []StUnionGodRecord{
		{
			PlayerID: 999, Name: "同尘", Platform: "qq", Server: "s0",
			A: 1, B: 34, Time: now.Add(-20 * time.Minute).Unix(),
		},
		{
			PlayerID: 1000, Name: "他人", Platform: "qq", Server: "s0",
			A: 1, B: 34, Time: now.Add(-48 * time.Hour).Unix(),
		},
	}
}

func TestParseStUnionGodInfo_RoundTrip(t *testing.T) {
	// 固定中午时间，避免测试在午夜前后跨日导致 OfferedToday 断言不稳定。
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	pkt := BuildStUnionGodInfoResultPacket(1, 3456, 2, 3, stUnionGodRecordsFixture(now))
	if pkt.ActionID != ActionIDStUnionGodInfo {
		t.Fatalf("action id = 0x%08X", pkt.ActionID)
	}
	info, err := ParseStUnionGodInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.Flag != 1 || info.Exp != 3456 || info.C != 2 || info.D != 3 {
		t.Fatalf("头部字段不符: %+v", info)
	}
	if len(info.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(info.Records))
	}
	got := info.Records[0]
	want := stUnionGodRecordsFixture(now)[0]
	if got.PlayerID != want.PlayerID || got.Name != want.Name ||
		got.Platform != want.Platform || got.Server != want.Server ||
		got.A != want.A || got.B != want.B || got.Time != want.Time {
		t.Fatalf("record = %+v, want %+v", got, want)
	}
	if !info.OfferedToday(999, nil, now) {
		t.Fatal("当天记录应判定为已上香")
	}
	if info.OfferedToday(1000, nil, now) {
		t.Fatal("他人两日前的记录不应判定为今日已上香")
	}
	if info.OfferedToday(999, nil, now.Add(24*time.Hour)) {
		t.Fatal("次日不应沿用昨日记录")
	}
	if info.OfferedToday(0, nil, now) {
		t.Fatal("非法 playerID 应返回 false")
	}
}

func TestParseStUnionGodInfo_CompressedPayload(t *testing.T) {
	now := time.Now()
	raw := BuildStUnionGodInfoResultPacket(0, 0, 0, 0, stUnionGodRecordsFixture(now)).Payload
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	info, err := ParseStUnionGodInfo(buf.Bytes())
	if err != nil {
		t.Fatalf("parse compressed: %v", err)
	}
	if len(info.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(info.Records))
	}
}

func TestParseStUnionGodInfo_Truncated(t *testing.T) {
	full := BuildStUnionGodInfoResultPacket(1, 1, 1, 1, stUnionGodRecordsFixture(time.Now())).Payload
	for _, n := range []int{0, 1, 4, 8, 10} {
		if n >= len(full) {
			continue
		}
		if _, err := ParseStUnionGodInfo(full[:n]); err == nil {
			t.Fatalf("截断 %d 字节应报错", n)
		}
	}
}

func TestStUnionGodInfo_NilSafe(t *testing.T) {
	var info *StUnionGodInfo
	if info.OfferedToday(999, nil, time.Now()) {
		t.Fatal("nil 面板不应判定为已上香")
	}
}

// liveStUnionGodPanelHex 是 2026-11-22 实机 179/10 回包（s0，len=339），
// 用于锁定记录字段宽度：时间戳是 Int32，不是 Int64。
const liveStUnionGodPanelHex = "070001912c00010008000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6b011f530000ae210006e5908ce5b0980006e5bf83e58aa800027330000000010000000a6af926cd000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6afae827000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6af33f84000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6b02c049000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6af93eef000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6afea728000007a5000ce5a4a7e59388e789b9e8998e0006e5bf83e58aa800027330000000010000000a6af66fa8"

func TestParseStUnionGodInfo_LivePanel(t *testing.T) {
	raw, err := hex.DecodeString(liveStUnionGodPanelHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	info, err := ParseStUnionGodInfo(raw)
	if err != nil {
		t.Fatalf("parse live panel: %v", err)
	}
	if info.Flag != 7 || info.Exp != 102700 || info.C != 0 || info.D != 1 {
		t.Fatalf("头部字段不符: %+v", info)
	}
	if len(info.Records) != 8 {
		t.Fatalf("records = %d, want 8", len(info.Records))
	}
	rec := info.Records[1]
	if rec.PlayerID != 44577 || rec.Name != "同尘" ||
		rec.Platform != "心动" || rec.Server != "s0" ||
		rec.A != 1 || rec.B != 10 || rec.Time != 1794713293 {
		t.Fatalf("record[1] = %+v", rec)
	}
	// 面板最新记录是 2026-11-22，2026-11-24 应判定为今日未上香。
	if info.OfferedToday(44577, nil, time.Unix(1795500000, 0)) {
		t.Fatal("11-22 的记录不应判定为 11-24 已上香")
	}
}

// TestStUnionGodInfo_OfferedTodayByName 验证记录内数字 ID 与会话 PlayerID
// 命名空间不一致时，仍能按显示名判断今日已上香。
func TestStUnionGodInfo_OfferedTodayByName(t *testing.T) {
	now := time.Now()
	pkt := BuildStUnionGodInfoResultPacket(1, 0, 0, 0, []StUnionGodRecord{
		{PlayerID: 44577, Name: "同尘", Platform: "心动", Server: "s0", Time: now.Unix()},
	})
	info, err := ParseStUnionGodInfo(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if info.OfferedToday(234797, nil, now) {
		t.Fatal("ID 不一致且未提供名称时不应命中")
	}
	if !info.OfferedToday(234797, []string{"同尘"}, now) {
		t.Fatal("应按显示名命中今日记录")
	}
	if info.OfferedToday(234797, []string{"他人"}, now) {
		t.Fatal("不匹配的名称不应命中")
	}
}

func TestParseStUnionTreeStatus(t *testing.T) {
	// 2026-11-24 实机跨服会话 179/0 回包首字节为 6 (HAS_FINISH)。
	const liveHex = "060000000000000002010003002d0012000008d70000000501002b00130000089500000001010028001100000994000000080100"
	raw, err := hex.DecodeString(liveHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	st, err := ParseStUnionTreeStatus(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if st != StUnionActivityHasFinish {
		t.Fatalf("status = %d, want %d", st, StUnionActivityHasFinish)
	}
	if _, err := ParseStUnionTreeStatus(nil); err == nil {
		t.Fatal("空载荷应报错")
	}
}
