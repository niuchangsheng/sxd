// Package protocol_test 提供问灵协议 (Module 503) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestAskFairyInfoResult(t *testing.T) {
	pkt := protocol.BuildAskFairyInfoResultPacket(protocol.AskFairyInfo{
		FirstLoginTime: 1335541013,
		TotalLoginDay:  3372,
		PartnerNum:     95,
		Level:          300,
		FightPower:     9540268288,
		HasGainAward:   1,
		MaxAskNum:      1,
		HasAskNum:      0,
		LeftDay:        28,
		MountCount:     38,
		TitleCount:     39,
		CurLevel:       6,
		RandomAwards: []protocol.AskFairyRandomAward{
			{AwardID: 1, ItemID: 3345, ItemCount: 20},
			{AwardID: 2, ItemID: 1747, ItemCount: 1000000},
		},
	})
	info, err := protocol.ParseAskFairyInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析问灵状态失败: %v", err)
	}
	if info.TotalLoginDay != 3372 || info.FightPower != 9540268288 || info.HasGainAward != 1 ||
		info.MaxAskNum != 1 || info.HasAskNum != 0 || info.LeftDay != 28 || info.CurLevel != 6 {
		t.Errorf("问灵状态不一致: %+v", info)
	}
	if len(info.RandomAwards) != 2 || info.RandomAwards[0].ItemID != 3345 || info.RandomAwards[0].ItemCount != 20 {
		t.Errorf("随机奖励候选不一致: %+v", info.RandomAwards)
	}
}

func TestAskFairyActionResult(t *testing.T) {
	// 成功路径
	result, err := protocol.ParseAskFairyActionResult(
		protocol.BuildAskFairyActionResultPacket(protocol.ActionIDAskFairyDo, protocol.AskFairyResultSuccess).Payload)
	if err != nil || result != protocol.AskFairyResultSuccess {
		t.Fatalf("期望成功码 0, got result=%d err=%v", result, err)
	}
	// 失败码路径: 今日已问灵必须能被识别 (回包解析铁律)
	result, err = protocol.ParseAskFairyActionResult(
		protocol.BuildAskFairyActionResultPacket(protocol.ActionIDAskFairyDo, protocol.AskFairyResultHasAsk).Payload)
	if err != nil || result != protocol.AskFairyResultHasAsk {
		t.Fatalf("期望已问灵码 %d, got result=%d err=%v", protocol.AskFairyResultHasAsk, result, err)
	}
	// 空回包必须报错
	if _, err := protocol.ParseAskFairyActionResult(nil); err == nil {
		t.Error("空问灵回包应返回解析错误")
	}
}

func TestAskFairyPackets(t *testing.T) {
	info := protocol.BuildAskFairyInfoPacket()
	if info.ActionID != protocol.ActionIDAskFairyInfo || len(info.Payload) != 0 {
		t.Errorf("问灵信息请求应为空载荷, got action=0x%08X payload=%v", info.ActionID, info.Payload)
	}
	gain := protocol.BuildAskFairyGainPacket(7)
	if gain.ActionID != protocol.ActionIDAskFairyGain || len(gain.Payload) != 4 {
		t.Errorf("问灵奖励请求应为 4 字节 award_id, got action=0x%08X payload=%v", gain.ActionID, gain.Payload)
	}
}
