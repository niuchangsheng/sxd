package daily

import (
	"context"
	"testing"
	"time"

	"sxd-pie-ng/internal/client"
	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// startFunctionEndTestSession 启动一个 mock 会话并等待其进入激活态。
func startFunctionEndTestSession(t *testing.T, roleID string) *client.RoleSession {
	t.Helper()
	session := client.NewRoleSession(client.SessionConfig{
		RoleID:     roleID,
		RoleName:   "功能终结礼包测试角色",
		ServerAddr: "mock",
	})
	if err := session.Start(context.Background()); err != nil {
		t.Fatalf("启动测试会话失败: %v", err)
	}
	t.Cleanup(session.Close)
	deadline := time.Now().Add(2 * time.Second)
	for session.State() != client.StateActive && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if session.State() != client.StateActive {
		t.Fatalf("会话未激活: %s", session.State())
	}
	return session
}

// fetchFunctionEndGiftIDs 直接查询 mock 服务端当前剩余的终结礼包 ID。
func fetchFunctionEndGiftIDs(t *testing.T, session *client.RoleSession) []int16 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resp, err := session.Call(ctx, protocol.BuildFunctionEndGiftListPacket(), protocol.ActionIDGameFunctionEndGift)
	if err != nil {
		t.Fatalf("查询终结礼包列表失败: %v", err)
	}
	gifts, err := protocol.ParseFunctionEndGiftListResult(resp.Payload)
	if err != nil {
		t.Fatalf("解析终结礼包列表失败: %v", err)
	}
	ids := make([]int16, len(gifts))
	for i, g := range gifts {
		ids[i] = g.GiftID
	}
	return ids
}

func TestOnlineGiftRoutine_ClaimsFreeFunctionEndGifts(t *testing.T) {
	session := startFunctionEndTestSession(t, "role_test_function_end_free")
	before := session.GetPlayerState().Ingots
	if err := NewOnlineGiftRoutine(DefaultOnlineGiftConfig()).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("在线礼包任务执行失败: %v", err)
	}

	// 免费领取按钮直接调用 get_game_function_end_gift，不依赖 random_award；
	// 列表中的 ingot 只是随机奖励按钮价格，不应阻止领取。
	ids := fetchFunctionEndGiftIDs(t, session)
	if len(ids) != 0 {
		t.Fatalf("期望全部免费礼包已领取, 实际剩余 %v", ids)
	}
	if after := session.GetPlayerState().Ingots; after != before {
		t.Fatalf("免费领取不应消耗元宝: before=%d after=%d", before, after)
	}
}

func TestOnlineGiftRoutine_ClaimsGiftsWithPaidRandomPrice(t *testing.T) {
	// 礼包 4/16 的随机奖励按钮标价 50/60 元宝，但普通领取按钮仍免费；
	// 自动化必须全部领取，且元宝不得变化。
	session := startFunctionEndTestSession(t, "role_test_function_end_no_ingot")
	before := session.GetPlayerState().Ingots
	for i := 0; i < 2; i++ {
		if err := NewOnlineGiftRoutine(DefaultOnlineGiftConfig()).Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("第 %d 轮在线礼包任务执行失败: %v", i+1, err)
		}
	}
	if ids := fetchFunctionEndGiftIDs(t, session); len(ids) != 0 {
		t.Fatalf("期望全部礼包已领取, 实际剩余 %v", ids)
	}
	if after := session.GetPlayerState().Ingots; after != before {
		t.Fatalf("领取功能终结礼包不应消耗元宝: before=%d after=%d", before, after)
	}
}

func TestOnlineGiftRoutine_SkipsFunctionEndGiftsWhenDisabled(t *testing.T) {
	session := startFunctionEndTestSession(t, "role_test_function_end_disabled")
	cfg := DefaultOnlineGiftConfig()
	cfg.ClaimFunctionEndGifts = false

	if err := NewOnlineGiftRoutine(cfg).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("在线礼包任务执行失败: %v", err)
	}

	if ids := fetchFunctionEndGiftIDs(t, session); len(ids) != 3 {
		t.Fatalf("关闭功能终结礼包后不应发起领取, 实际剩余 %v", ids)
	}
}

func TestOnlineGiftRoutine_FunctionEndPackFullCircuitBreaker(t *testing.T) {
	t.Setenv("SXD_MOCK_FUNCTION_END_PACK_FULL", "1")
	session := startFunctionEndTestSession(t, "role_test_function_end_pack_full")
	cfg := DefaultOnlineGiftConfig()

	err := NewOnlineGiftRoutine(cfg).Execute(context.Background(), session, nil)
	if err != scheduler.ErrBagFull {
		t.Fatalf("背包已满时期望返回 ErrBagFull, 实际 %v", err)
	}
}

func TestFunctionEndClaimGranted(t *testing.T) {
	cases := []struct {
		name  string
		claim *protocol.FunctionEndClaimResult
		want  bool
	}{
		{name: "nil", claim: nil, want: false},
		{name: "全零奖励", claim: &protocol.FunctionEndClaimResult{IsHaveNext: true}, want: false},
		{name: "境界点", claim: &protocol.FunctionEndClaimResult{StatePoint: 10}, want: true},
		{name: "技能点", claim: &protocol.FunctionEndClaimResult{Skill: 1}, want: true},
		{name: "聚灵", claim: &protocol.FunctionEndClaimResult{JuLing: 5}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := functionEndClaimGranted(tc.claim); got != tc.want {
				t.Fatalf("functionEndClaimGranted=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestOnlineGiftRoutine_PreservesOnlineGiftStateMachine(t *testing.T) {
	session := startFunctionEndTestSession(t, "role_test_function_end_online_state")
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.IsGetOnlineGift = -1 // 活动未开放
	})

	// 即使在线礼包不可领取，也应正常完成功能终结礼包领取且不返回错误。
	if err := NewOnlineGiftRoutine(DefaultOnlineGiftConfig()).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("在线礼包不可领取时任务不应失败: %v", err)
	}
	if ids := fetchFunctionEndGiftIDs(t, session); len(ids) != 0 {
		t.Fatalf("免费功能终结礼包应全部领取, 剩余 %v", ids)
	}
}

// TestFunctionEndMock_ClaimWithoutRandomAwardReturnsEmpty 固化服务端语义：
// 未先 random_award(127/3) 时，127/2 返回全 0 奖励且 is_have_next=1。
func TestFunctionEndMock_ClaimWithoutRandomAwardReturnsEmpty(t *testing.T) {
	session := startFunctionEndTestSession(t, "role_test_function_end_no_roll")
	defer session.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := session.Call(ctx, protocol.BuildGetFunctionEndGiftPacket(1), protocol.ActionIDGetGameFunctionEndGift)
	if err != nil {
		t.Fatalf("claim without roll: %v", err)
	}
	claim, err := protocol.ParseGetGameFunctionEndGiftResult(resp.Payload)
	if err != nil {
		t.Fatalf("parse claim: %v", err)
	}
	if !claim.IsHaveNext {
		t.Fatal("未摇奖时应返回 is_have_next=true")
	}
	if claim.StatePoint != 0 || claim.Skill != 0 || claim.Nimbus != 0 || claim.JuLing != 0 {
		t.Fatalf("未摇奖时奖励应全 0, got %+v", claim)
	}
}
