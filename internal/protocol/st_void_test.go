// Package protocol 提供仙界虚空协议编解码测试。
//
// @author Ateng
// @since 2026-11-08
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestStVoidOpenAndLikePackets(t *testing.T) {
	open := BuildStVoidOpenPacket()
	if open.ActionID != ActionIDStVoidOpen || len(open.Payload) != 0 {
		t.Fatalf("open_panel 封包不符: %+v", open)
	}
	like := BuildStVoidLikePacket(372164)
	if like.ActionID != ActionIDStVoidLike {
		t.Fatalf("like 封包动作不符: %+v", like)
	}
	// 官方 request=[Int st_uid]，不带 PrevAct。
	if got := hex.EncodeToString(like.Payload); got != "0005adc4" {
		t.Fatalf("like 封包载荷不符: %s", got)
	}
}

func TestStVoidPanelResultRoundTrip(t *testing.T) {
	want := StVoidPanel{
		Result: StVoidResultSuccess,
		StName: "st-void",
		StUID:  100,
		Members: []StVoidMember{
			{StID: 1, StUID: 200, MatchRank: 1, LikeNum: 9, ServerName: "s1", Nickname: "甲", RoleID: 1, ClothID: 2, Level: 80, WarPower: 123456, WorldPKScore: 300},
			{StID: 2, StUID: 300, MatchRank: 2, LikeNum: 3, ServerName: "s2", Nickname: "乙", RoleID: 3, ClothID: 4, Level: 79, WarPower: 654321, WorldPKScore: 200},
		},
		LikedUIDs: []int32{200},
	}
	got, err := ParseStVoidPanelResult(BuildStVoidPanelResultPacket(want).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.StName != want.StName || got.StUID != want.StUID || len(got.Members) != 2 || len(got.LikedUIDs) != 1 {
		t.Fatalf("面板解析字段不符: %+v", got)
	}
	if got.Members[0].StUID != 200 || got.Members[0].WarPower != 123456 || got.Members[1].Nickname != "乙" {
		t.Fatalf("成员解析不符: %+v", got.Members)
	}
	if got.LikedUIDs[0] != 200 {
		t.Fatalf("已膜拜列表不符: %+v", got.LikedUIDs)
	}
}

func TestStVoidLikeResult(t *testing.T) {
	got, err := ParseStVoidLikeResult(BuildStVoidLikeResultPacket(StVoidLikeResult{
		Result: StVoidResultSuccess,
		Awards: []StVoidAward{{ItemID: 2016, Num: 1}, {ItemID: 7262, Num: 1}},
	}).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Result != StVoidResultSuccess || len(got.Awards) != 2 || got.Awards[1].ItemID != 7262 {
		t.Fatalf("膜拜奖励解析不符: %+v", got)
	}

	fail, err := ParseStVoidLikeResult(BuildStVoidLikeResultPacket(StVoidLikeResult{
		Result: StVoidResultIsLiked,
	}).Payload)
	if err != nil || fail.Result != StVoidResultIsLiked {
		t.Fatalf("失败回包解析不符: %+v err=%v", fail, err)
	}
	if text := StVoidResultText(StVoidResultIsLiked); text != "本周已膜拜" {
		t.Fatalf("结果码文本不符: %s", text)
	}
}
