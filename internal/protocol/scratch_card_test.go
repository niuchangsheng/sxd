// Package protocol_test 提供幸运刮刮卡协议 (Module 185) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestScratchCardInfoResult(t *testing.T) {
	pkt := protocol.BuildScratchCardInfoResultPacket(protocol.ScratchCardInfo{
		InActivity:   1,
		EndTime:      1800000000,
		ActiveDegree: 20,
		CardTimes:    2,
	})
	info, err := protocol.ParseScratchCardInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析刮刮卡状态失败: %v", err)
	}
	if info.InActivity != 1 || info.EndTime != 1800000000 || info.ActiveDegree != 20 || info.CardTimes != 2 {
		t.Errorf("刮刮卡状态不一致: %+v", info)
	}
}

func TestScratchCardShaveResult(t *testing.T) {
	// 元宝奖励路径
	res, err := protocol.ParseScratchCardShaveResult(
		protocol.BuildScratchCardShaveResultPacket(protocol.ScratchCardResultIngot, 3345).Payload)
	if err != nil {
		t.Fatalf("解析刮卡回包失败: %v", err)
	}
	if res.Result != protocol.ScratchCardResultIngot || res.ItemID != 3345 {
		t.Errorf("元宝刮卡结果不一致: %+v", res)
	}

	// 失败码路径: 次数不足必须能被识别 (回包解析铁律)
	res, err = protocol.ParseScratchCardShaveResult(
		protocol.BuildScratchCardShaveResultPacket(protocol.ScratchCardResultNotEnoughTimes, 0).Payload)
	if err != nil {
		t.Fatalf("解析次数不足回包失败: %v", err)
	}
	if res.Result != protocol.ScratchCardResultNotEnoughTimes {
		t.Errorf("期望次数不足码 %d, got %d", protocol.ScratchCardResultNotEnoughTimes, res.Result)
	}
}
