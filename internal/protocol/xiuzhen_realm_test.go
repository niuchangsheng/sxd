package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestParseXiuZhenRealmResult 验证 552/5 礼包回包的奖励列表解析
// （抓包奖励：8808 青铜宝箱 ×5、5852 一品神器包 ×1）。
func TestParseXiuZhenRealmResult(t *testing.T) {
	pkt := protocol.BuildXiuZhenRealmResultPacket(protocol.XiuZhenRealmSuccess,
		[]protocol.XiuZhenRealmReward{
			{ItemID: 8808, Num: 5},
			{ItemID: 5852, Num: 1},
		})
	got, err := protocol.ParseXiuZhenRealmResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Result != protocol.XiuZhenRealmSuccess {
		t.Fatalf("result = %d", got.Result)
	}
	if len(got.Rewards) != 2 {
		t.Fatalf("rewards = %+v", got.Rewards)
	}
	if got.Rewards[0] != (protocol.XiuZhenRealmReward{ItemID: 8808, Num: 5}) ||
		got.Rewards[1] != (protocol.XiuZhenRealmReward{ItemID: 5852, Num: 1}) {
		t.Fatalf("rewards = %+v", got.Rewards)
	}
}

// TestParseXiuZhenRealmResult_ErrorCode 验证失败码不带奖励列表。
func TestParseXiuZhenRealmResult_ErrorCode(t *testing.T) {
	pkt := protocol.BuildXiuZhenRealmResultPacket(protocol.XiuZhenRealmIsGet, nil)
	got, err := protocol.ParseXiuZhenRealmResult(pkt.Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Result != protocol.XiuZhenRealmIsGet || len(got.Rewards) != 0 {
		t.Fatalf("got %+v", got)
	}
}

// TestBuildXiuZhenRealmPackets 验证 552/1、552/5 均为空参数 + 4 字节 PrevAct。
func TestBuildXiuZhenRealmPackets(t *testing.T) {
	mainPkt := protocol.BuildXiuZhenRealmMainPacket()
	if mainPkt.ActionID != protocol.ActionIDXiuZhenRealmMain || len(mainPkt.Payload) != 4 ||
		!mainPkt.PrevActFromLastSent {
		t.Fatalf("552/1 = %+v", mainPkt)
	}
	awardPkt := protocol.BuildXiuZhenRealmExtraAwardPacket()
	if awardPkt.ActionID != protocol.ActionIDXiuZhenRealmExtraAward || len(awardPkt.Payload) != 4 ||
		!awardPkt.PrevActFromLastSent {
		t.Fatalf("552/5 = %+v", awardPkt)
	}
}
