// Package protocol_test 提供日常签到协议编解码的测试用例。
//
// @author Ateng
// @since 2026-10-11
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestDailySignIn_Codec(t *testing.T) {
	req := protocol.DailySignInRequest{
		PrevAct: protocol.ActionIDPlayerGetInfo,
	}
	pkt := protocol.BuildDailySignInPacket(req)
	if pkt.ActionID != protocol.ActionIDDailySignIn {
		t.Fatalf("ActionID 不匹配: 0x%08X", pkt.ActionID)
	}

	parsedReq, err := protocol.ParseDailySignInRequest(pkt.Payload)
	if err != nil {
		t.Fatalf("解析签到请求失败: %v", err)
	}
	if parsedReq.PrevAct != protocol.ActionIDPlayerGetInfo {
		t.Errorf("期望 PrevAct 0x%08X, 实际 0x%08X", protocol.ActionIDPlayerGetInfo, parsedReq.PrevAct)
	}

	res := protocol.DailySignInResult{
		Success:       true,
		AlreadySigned: false,
		SignedDays:    15,
		RewardCoins:   100000,
		RewardIngots:  50,
		Message:       "签到成功，获得10万铜钱与50元宝！",
	}
	resPkt := protocol.BuildDailySignInResultPacket(res)
	parsedRes, err := protocol.ParseDailySignInResult(resPkt.Payload)
	if err != nil {
		t.Fatalf("解析签到响应失败: %v", err)
	}
	if !parsedRes.Success || parsedRes.AlreadySigned || parsedRes.SignedDays != 15 || parsedRes.RewardCoins != 100000 || parsedRes.RewardIngots != 50 {
		t.Errorf("解析结果与原始值不一致: %+v", parsedRes)
	}
}
