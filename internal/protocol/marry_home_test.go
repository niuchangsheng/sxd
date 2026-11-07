// Package protocol 提供家园奖励协议编解码测试。
//
// @author Ateng
// @since 2026-11-07
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestMarryHomeGetCakePacket(t *testing.T) {
	pkt := BuildMarryHomeGetCakePacket(106732, 0)
	if pkt.ActionID != ActionIDMarryHomeGetCake || !pkt.HasPrevAct {
		t.Fatalf("封包类型不符: %+v", pkt)
	}
	// Int32 player_id + Uint32 PrevAct
	if len(pkt.Payload) != 8 {
		t.Fatalf("载荷长度不符: %d", len(pkt.Payload))
	}
	want := "0001a0ec00010000"
	if got := hex.EncodeToString(pkt.Payload); got != want {
		t.Fatalf("载荷不符: got=%s want=%s", got, want)
	}
}

func TestMarryHomeCakeResult(t *testing.T) {
	okPkt := BuildMarryHomeCakeResultPacket(MarryHomeCakeResult{
		Result: MarryHomeResultSuccess,
		Awards: []MarryHomeCakeAward{{ItemID: 2001, ItemNum: 2}, {ItemID: 347, ItemNum: 5}},
	})
	res, err := ParseMarryHomeCakeResult(okPkt.Payload)
	if err != nil {
		t.Fatalf("解析成功回包失败: %v", err)
	}
	if res.Result != MarryHomeResultSuccess || len(res.Awards) != 2 ||
		res.Awards[0].ItemID != 2001 || res.Awards[1].ItemNum != 5 {
		t.Fatalf("成功回包解析不符: %+v", res)
	}

	// 失败码回包可能不带奖励列表，必须能透传结果码。
	failPkt := BuildMarryHomeCakeResultPacket(MarryHomeCakeResult{Result: MarryHomeResultNonActivityTime})
	fail, err := ParseMarryHomeCakeResult(failPkt.Payload)
	if err != nil {
		t.Fatalf("解析失败回包失败: %v", err)
	}
	if fail.Result != MarryHomeResultNonActivityTime || len(fail.Awards) != 0 {
		t.Fatalf("失败回包解析不符: %+v", fail)
	}

	// 线上实际失败回包只有 1 字节结果码。
	short, err := ParseMarryHomeCakeResult([]byte{MarryHomeResultNonActivityTime})
	if err != nil || short.Result != MarryHomeResultNonActivityTime {
		t.Fatalf("单字节失败回包解析不符: %+v err=%v", short, err)
	}

	if _, err := ParseMarryHomeCakeResult(nil); err == nil {
		t.Fatal("空载荷应返回错误")
	}
}