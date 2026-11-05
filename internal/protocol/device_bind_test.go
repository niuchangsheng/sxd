// Package protocol_test 提供设备绑定奖励协议 (Module 814) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestDeviceBindInfoResult(t *testing.T) {
	pkt := protocol.BuildDeviceBindInfoResultPacket(protocol.DeviceBindInfo{
		Result:         protocol.DeviceBindResultSuccess,
		IsBind:         1,
		PhoneNum:       "138****0000",
		FirstReward:    0,
		MonthlyReward:  1,
		VerifyCD:       60,
		NeedVerifyBind: 0,
		StartTime:      1743436800,
	})
	info, err := protocol.ParseDeviceBindInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析设备绑定状态失败: %v", err)
	}
	if info.Result != protocol.DeviceBindResultSuccess || info.IsBind != 1 ||
		info.PhoneNum != "138****0000" || info.FirstReward != 0 || info.MonthlyReward != 1 ||
		info.VerifyCD != 60 || info.NeedVerifyBind != 0 || info.StartTime != 1743436800 {
		t.Errorf("设备绑定状态不一致: %+v", info)
	}
}

func TestDeviceBindRewardResult(t *testing.T) {
	// 成功路径
	result, err := protocol.ParseDeviceBindRewardResult(
		protocol.BuildDeviceBindRewardResultPacket(protocol.DeviceBindResultSuccess).Payload)
	if err != nil || result != protocol.DeviceBindResultSuccess {
		t.Fatalf("期望成功码 0, got result=%d err=%v", result, err)
	}
	// 失败码路径: 已领取必须能被识别 (回包解析铁律)
	result, err = protocol.ParseDeviceBindRewardResult(
		protocol.BuildDeviceBindRewardResultPacket(protocol.DeviceBindResultAlreadyGet).Payload)
	if err != nil || result != protocol.DeviceBindResultAlreadyGet {
		t.Fatalf("期望已领取码 %d, got result=%d err=%v", protocol.DeviceBindResultAlreadyGet, result, err)
	}
	// 空回包必须报错
	if _, err := protocol.ParseDeviceBindRewardResult(nil); err == nil {
		t.Error("空设备绑定奖励回包应返回解析错误")
	}
}
