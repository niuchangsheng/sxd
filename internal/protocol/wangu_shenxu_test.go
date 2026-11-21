package protocol

import (
	"encoding/hex"
	"testing"
)

// wanguShenxuGoldenPanelHex 为 2026-11-21 抓包 /tmp/wangushenxu.pcapng 中
// 840/0 首个主面板回包 (zlib 整包解压后的 payload，685 字节)。
const wanguShenxuGoldenPanelHex = "00000500000004000000010000000000000000000000000000000000000000000000000000000000000000000001000000010000000000000000000000000000000000000000000000030000000f000000000003f4a700027330000fe58fb8e7a9bae6b58be8af95e58fb70000006d00001097000000004511bb3f0000009571000f0000000100000000000000020000000000000003000000000000000400000000000000050000000000000006000000000000000700000000000000080000000000000009000000000000000a000000000000000b000000000000000c000000000000000d000000000000000e000000000000000f00000000000000000bebc2000000000053724e0000000002000000030000000000000000000000000000000000000000000000000000000000000000000003000000010000000000000002000000000000000300000000000000000000000000000000000000000000000500000005000001fc0003f4a700027330000fe58fb8e7a9bae6b58be8af95e58fb70000006d000010970000000045996d080000001231000f00000001000001fa00000001000001fb00000001000001fc00000005000001fa00000005000001fb00000005000001fc00000002000001fa00000002000001fb00000002000001fc00000003000001fa00000003000001fb00000003000001fc00000004000001fa00000004000001fb00000004000001fc0000000005f5e1000000000017d784000000000100000007000000000003f4a700027330000fe58fb8e7a9bae6b58be8af95e58fb70000006d00001097000000003d29d45a00000032a1000700000001000000000000000200000000000000030000000000000004000000000000000500000000000000060000000000000007000000000000000005f5e100000000004190ab000000000000000000"

func TestParseWanguShenxuPanel_GoldenCapture(t *testing.T) {
	raw, err := hex.DecodeString(wanguShenxuGoldenPanelHex)
	if err != nil {
		t.Fatalf("golden hex 解码失败: %v", err)
	}
	panel, err := ParseWanguShenxuPanel(raw)
	if err != nil {
		t.Fatalf("解析主面板失败: %v", err)
	}
	if panel.Result != WanguShenxuResultSuccess {
		t.Fatalf("result = %d, want %d", panel.Result, WanguShenxuResultSuccess)
	}
	if len(panel.Players) != 5 {
		t.Fatalf("大神像数量 = %d, want 5", len(panel.Players))
	}
	if panel.CanGetCoin != 0 {
		t.Fatalf("can_get_coin = %d, want 0", panel.CanGetCoin)
	}

	// 空位: type=4 无膜拜对象，例程必须跳过。
	if got := panel.Players[0]; got.Type != 4 || got.PlayerID != 0 || got.IsWorship {
		t.Fatalf("空位解析异常: %+v", got)
	}

	// 第 2 个神像: 抓包中首次膜拜的 type=3。
	p := panel.Players[1]
	if p.Type != 3 || p.Season != 15 || p.Step != 0 {
		t.Fatalf("type=3 神像赛季字段异常: %+v", p)
	}
	if p.PlayerID != 259239 || p.ServerName != "s0" || p.Nickname != "司空测试号" ||
		p.RoleID != 109 || p.SuitID != 4247 || p.WarPower != 1158789951 {
		t.Fatalf("type=3 神像玩家字段异常: %+v", p)
	}
	if p.IsWorship || p.WorshipNum != 38257 {
		t.Fatalf("type=3 神像膜拜字段异常: %+v", p)
	}
	if len(p.Seasons) != 15 || p.Seasons[14] != (WanguShenxuSeason{Season: 15, Step: 0}) {
		t.Fatalf("type=3 seasons 异常: %+v", p.Seasons)
	}
	if p.WeedAwardCoin != 200000000 || p.TotalAwardCoin != 1400000000 {
		t.Fatalf("type=3 神像奖励字段异常: %+v", p)
	}

	// type=5 的赛季阶段字段用于排行榜/阶段展示，验证 step 解析。
	p5 := panel.Players[3]
	if p5.Type != 5 || p5.Season != 5 || p5.Step != 508 || p5.WorshipNum != 4657 {
		t.Fatalf("type=5 神像字段异常: %+v", p5)
	}
	if len(p5.Seasons) != 15 || p5.Seasons[2] != (WanguShenxuSeason{Season: 1, Step: 508}) {
		t.Fatalf("type=5 seasons 异常: %+v", p5.Seasons)
	}
}

func TestParseWanguShenxuPanel_AlreadyWorshippedAndAward(t *testing.T) {
	w := NewWriter()
	w.WriteUint8(WanguShenxuResultSuccess)
	w.WriteUint16(1)
	w.WriteInt32(3)          // type
	w.WriteInt32(15)         // season
	w.WriteInt32(0)          // step
	w.WriteInt32(259239)     // player_id
	w.WriteString("s0")      // server_name
	w.WriteString("司空测试号")   // nickname
	w.WriteInt32(109)        // role_id
	w.WriteInt32(4247)       // suit_id
	w.WriteInt64(1158789951) // war_power
	w.WriteUint8(1)          // is_worship
	w.WriteInt32(38258)      // worship_num
	w.WriteUint16(1)
	w.WriteInt32(7)
	w.WriteInt32(0)
	w.WriteInt64(200000000)
	w.WriteInt64(1400000000)
	w.WriteInt64(987654321) // can_get_coin

	panel, err := ParseWanguShenxuPanel(w.Bytes())
	if err != nil {
		t.Fatalf("解析主面板失败: %v", err)
	}
	if len(panel.Players) != 1 {
		t.Fatalf("玩家数量 = %d, want 1", len(panel.Players))
	}
	if !panel.Players[0].IsWorship {
		t.Fatal("is_worship=1 应解析为 true")
	}
	if panel.CanGetCoin != 987654321 {
		t.Fatalf("can_get_coin = %d, want 987654321", panel.CanGetCoin)
	}
}

func TestParseWanguShenxuPanel_FailureResult(t *testing.T) {
	panel, err := ParseWanguShenxuPanel([]byte{WanguShenxuResultFuncNotOpen})
	if err != nil {
		t.Fatalf("失败结果码不应返回解析错误: %v", err)
	}
	if panel.Result != WanguShenxuResultFuncNotOpen || len(panel.Players) != 0 {
		t.Fatalf("失败面板解析异常: %+v", panel)
	}
}

func TestParseWanguShenxuWorshipResult(t *testing.T) {
	got, err := ParseWanguShenxuWorshipResult([]byte{0, 0, 0, 0, 5})
	if err != nil {
		t.Fatalf("解析膜拜成功回包失败: %v", err)
	}
	if got.Result != WanguShenxuResultSuccess || got.Ingot != 5 {
		t.Fatalf("膜拜成功回包 = %+v, want result=0 ingot=5", got)
	}

	got, err = ParseWanguShenxuWorshipResult([]byte{WanguShenxuResultHasWorshipped})
	if err != nil {
		t.Fatalf("今日已膜拜不应报错: %v", err)
	}
	if got.Result != WanguShenxuResultHasWorshipped || got.Ingot != 0 {
		t.Fatalf("今日已膜拜回包 = %+v", got)
	}

	if _, err = ParseWanguShenxuWorshipResult([]byte{0, 0}); err == nil {
		t.Fatal("成功回包缺少 Int 元宝字段时应报错")
	}
	if _, err = ParseWanguShenxuWorshipResult(nil); err == nil {
		t.Fatal("空回包应报错")
	}
}

func TestParseWanguShenxuAwardResult(t *testing.T) {
	w := NewWriter()
	w.WriteUint8(WanguShenxuResultSuccess)
	w.WriteInt64(123456)
	got, err := ParseWanguShenxuAwardResult(w.Bytes())
	if err != nil {
		t.Fatalf("解析领奖成功回包失败: %v", err)
	}
	if got.Result != WanguShenxuResultSuccess || got.Coin != 123456 {
		t.Fatalf("领奖成功回包 = %+v", got)
	}

	got, err = ParseWanguShenxuAwardResult([]byte{WanguShenxuResultNotAward})
	if err != nil {
		t.Fatalf("暂无可领取奖励不应报错: %v", err)
	}
	if got.Result != WanguShenxuResultNotAward || got.Coin != 0 {
		t.Fatalf("暂无可领取奖励回包 = %+v", got)
	}

	if _, err = ParseWanguShenxuAwardResult([]byte{0, 0, 0}); err == nil {
		t.Fatal("成功回包缺少 Long 铜钱字段时应报错")
	}
}

func TestBuildWanguShenxuPackets(t *testing.T) {
	info := BuildWanguShenxuMainInfoPacket()
	if info.ActionID != ActionIDWanguShenxuMainInfo {
		t.Fatalf("main_info action = 0x%08X", info.ActionID)
	}
	if len(info.Payload) != 4 || !info.HasPrevAct {
		t.Fatalf("main_info 载荷应为 4 字节 PrevAct 占位: len=%d hasPrevAct=%v", len(info.Payload), info.HasPrevAct)
	}

	worship := BuildWanguShenxuWorshipPacket(5)
	if worship.ActionID != ActionIDWanguShenxuWorship {
		t.Fatalf("worship action = 0x%08X", worship.ActionID)
	}
	if len(worship.Payload) != 8 || !worship.HasPrevAct {
		t.Fatalf("worship 载荷应为 [Int type][PrevAct]: len=%d hasPrevAct=%v", len(worship.Payload), worship.HasPrevAct)
	}
	if typ := int32(uint32(worship.Payload[0])<<24 | uint32(worship.Payload[1])<<16 |
		uint32(worship.Payload[2])<<8 | uint32(worship.Payload[3])); typ != 5 {
		t.Fatalf("worship type = %d, want 5", typ)
	}

	award := BuildWanguShenxuGetAwardPacket()
	if award.ActionID != ActionIDWanguShenxuGetAward {
		t.Fatalf("get_award action = 0x%08X", award.ActionID)
	}
	if len(award.Payload) != 4 || !award.HasPrevAct {
		t.Fatalf("get_award 载荷应为 4 字节 PrevAct 占位: len=%d hasPrevAct=%v", len(award.Payload), award.HasPrevAct)
	}
}

func TestWanguShenxuResultText(t *testing.T) {
	cases := map[uint8]string{
		WanguShenxuResultSuccess:       "成功",
		WanguShenxuResultFailed:        "失败",
		WanguShenxuResultFuncNotOpen:   "功能未开放",
		WanguShenxuResultHasWorshipped: "今日已膜拜",
		WanguShenxuResultNotAward:      "暂无可领取奖励",
	}
	for code, want := range cases {
		if got := WanguShenxuResultText(code); got != want {
			t.Errorf("WanguShenxuResultText(%d) = %q, want %q", code, got, want)
		}
	}
	if got := WanguShenxuResultText(99); got != "未知结果码(99)" {
		t.Errorf("未知结果码文本 = %q", got)
	}
}
