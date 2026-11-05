// Package protocol_test 提供日常签到协议 (Module 89, Mod_SignIn_Base) 的测试用例。
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestSignInStatusResult(t *testing.T) {
	open, err := protocol.ParseSignInStatusResult(protocol.BuildSignInStatusResultPacket(0, 1, 1).Payload)
	if err != nil {
		t.Fatalf("解析签到状态失败: %v", err)
	}
	if !open.ActivityOpen || !open.HasAward {
		t.Errorf("期望活动开启且有奖, got %+v", open)
	}

	closed, err := protocol.ParseSignInStatusResult(protocol.BuildSignInStatusResultPacket(0, 0, 0).Payload)
	if err != nil {
		t.Fatalf("解析签到状态失败: %v", err)
	}
	if closed.ActivityOpen {
		t.Errorf("期望活动未开启, got %+v", closed)
	}
}

func TestSignInInfoResult(t *testing.T) {
	pkt := protocol.BuildSignInInfoResultPacket(protocol.SignInInfo{
		ContinueState: protocol.SignInStateAlreadyGet,
		ContinueDay:   3,
		TotalSignIn:   []int32{1, 2, 3},
		TotalInfo: []protocol.SignInTier{
			{Tier: 2, State: protocol.SignInStateCanGet},
			{Tier: 3, State: protocol.SignInStateAlreadyGet},
		},
		TotalDay:   3,
		SignState:  protocol.SignInStateNoSignIn,
		RepairSign: 5,
		CurDay:     6,
	})
	info, err := protocol.ParseSignInInfoResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析签到详情失败: %v", err)
	}
	if info.ContinueDay != 3 || info.TotalDay != 3 || info.SignState != protocol.SignInStateNoSignIn ||
		info.RepairSign != 5 || info.CurDay != 6 || len(info.TotalSignIn) != 3 || len(info.TotalInfo) != 2 {
		t.Fatalf("签到详情不一致: %+v", info)
	}
	if info.TotalInfo[0].Tier != 2 || info.TotalInfo[0].State != protocol.SignInStateCanGet {
		t.Errorf("档位状态不一致: %+v", info.TotalInfo[0])
	}
}

func TestSignInActionResult_SuccessAndFailure(t *testing.T) {
	// 成功路径: SI_SUCCESS=12，player_sign_in 无 sign_state 尾部。
	okPkt := protocol.BuildSignInActionResultPacket(protocol.SignInActionResult{
		Result:    protocol.SignInResultOK,
		TotalDay:  4,
		TotalInfo: []protocol.SignInTier{{Tier: 2, State: protocol.SignInStateCanGet}},
	}, false)
	ok, err := protocol.ParseSignInActionResult(okPkt.Payload)
	if err != nil {
		t.Fatalf("解析签到成功回包失败: %v", err)
	}
	if ok.Result != protocol.SignInResultOK || ok.TotalDay != 4 {
		t.Errorf("签到成功回包不一致: %+v", ok)
	}

	// 失败码路径: SI_FAILED=13 必须能被识别 (回包解析铁律)。
	failPkt := protocol.BuildSignInActionResultPacket(protocol.SignInActionResult{
		Result: protocol.SignInResultFail,
	}, false)
	fail, err := protocol.ParseSignInActionResult(failPkt.Payload)
	if err != nil {
		t.Fatalf("解析签到失败回包失败: %v", err)
	}
	if fail.Result != protocol.SignInResultFail {
		t.Errorf("期望失败码 %d, got %d", protocol.SignInResultFail, fail.Result)
	}
}

func TestSignInContinueResult(t *testing.T) {
	pkt := protocol.BuildSignInContinueResultPacket(protocol.SignInResultFailed, 2)
	result, day, err := protocol.ParseSignInContinueResult(pkt.Payload)
	if err != nil {
		t.Fatalf("解析连签奖励回包失败: %v", err)
	}
	if result != protocol.SignInResultFailed || day != 2 {
		t.Errorf("连签奖励回包不一致: result=%d day=%d", result, day)
	}
}

func TestSignInPackets(t *testing.T) {
	cases := []struct {
		pkt      *protocol.Packet
		actionID uint32
		len      int
	}{
		{protocol.BuildSignInStatusPacket(), protocol.ActionIDSignInStatus, 0},
		{protocol.BuildSignInInfoPacket(), protocol.ActionIDSignInInfo, 0},
		{protocol.BuildSignInPlayerPacket(), protocol.ActionIDSignInPlayer, 0},
		{protocol.BuildSignInContinuePacket(), protocol.ActionIDSignInContinue, 0},
		{protocol.BuildSignInTotalAwardPacket(3), protocol.ActionIDSignInTotalAward, 1},
	}
	for _, c := range cases {
		if c.pkt.ActionID != c.actionID || len(c.pkt.Payload) != c.len {
			t.Errorf("封包不匹配: action=0x%08X len=%d, want 0x%08X len=%d",
				c.pkt.ActionID, len(c.pkt.Payload), c.actionID, c.len)
		}
	}
}
