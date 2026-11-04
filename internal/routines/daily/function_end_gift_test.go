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
	if err := NewOnlineGiftRoutine(DefaultOnlineGiftConfig()).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("在线礼包任务执行失败: %v", err)
	}

	// mock 中只有 gift 1 摇奖价格为 0，可领 2 次；
	// gift 4（50 元宝）与 gift 16（60 元宝）摇奖后必须跳过。
	ids := fetchFunctionEndGiftIDs(t, session)
	if len(ids) != 2 || ids[0] != 4 || ids[1] != 16 {
		t.Fatalf("期望仅剩需要元宝的 gift 4/16, 实际剩余 %v", ids)
	}
}

func TestOnlineGiftRoutine_AlwaysSkipsIngotCostGift(t *testing.T) {
	// 铁律: 自动化永不消耗元宝。摇奖价格为 0 的 gift 1 可领，
	// 价格为 50/60 的 gift 4/16 绝不能被领取。
	session := startFunctionEndTestSession(t, "role_test_function_end_no_ingot")
	for i := 0; i < 2; i++ {
		if err := NewOnlineGiftRoutine(DefaultOnlineGiftConfig()).Execute(context.Background(), session, nil); err != nil {
			t.Fatalf("第 %d 轮在线礼包任务执行失败: %v", i+1, err)
		}
		if ids := fetchFunctionEndGiftIDs(t, session); len(ids) != 2 || ids[0] != 4 || ids[1] != 16 {
			t.Fatalf("第 %d 轮后期望仅剩需要元宝的 gift 4/16, 实际剩余 %v", i+1, ids)
		}
	}
}

func TestFunctionEndGiftHasPendingRoll(t *testing.T) {
	if functionEndGiftHasPendingRoll(protocol.FunctionEndGift{GiftID: 16}) {
		t.Fatal("空记录不应判定为已摇奖")
	}
	if !functionEndGiftHasPendingRoll(protocol.FunctionEndGift{GiftID: 16, Nimbus: 43}) {
		t.Fatal("带奖励字段的记录应判定为已摇奖")
	}
}

func TestFunctionEndRollIsFree(t *testing.T) {
	if !functionEndRollIsFree(&protocol.FunctionEndRandomAward{Ingot: 0}) {
		t.Fatal("ingot=0 应判定为免费")
	}
	if functionEndRollIsFree(&protocol.FunctionEndRandomAward{Ingot: 50}) {
		t.Fatal("ingot=50 应判定为付费")
	}
	if functionEndRollIsFree(nil) {
		t.Fatal("nil 预览应判定为不可领取")
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

func TestOnlineGiftRoutine_PreservesOnlineGiftStateMachine(t *testing.T) {
	session := startFunctionEndTestSession(t, "role_test_function_end_online_state")
	session.UpdatePlayerState(func(ps *client.PlayerState) {
		ps.IsGetOnlineGift = -1 // 活动未开放
	})

	// 即使在线礼包不可领取，也应正常完成功能终结礼包领取且不返回错误。
	if err := NewOnlineGiftRoutine(DefaultOnlineGiftConfig()).Execute(context.Background(), session, nil); err != nil {
		t.Fatalf("在线礼包不可领取时任务不应失败: %v", err)
	}
	if ids := fetchFunctionEndGiftIDs(t, session); len(ids) != 2 || ids[0] != 4 || ids[1] != 16 {
		t.Fatalf("功能终结礼包领取结果异常, 剩余 %v", ids)
	}
}
