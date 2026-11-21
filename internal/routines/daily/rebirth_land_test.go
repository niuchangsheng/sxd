package daily

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func eraDrawPayload(result uint8) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(1)
	w.WriteInt32(4)
	return w.Bytes()
}

// eraFreeBackPayload 构造 852/6、852/8 共用的回包 [UByte result][UInt16 count][Int,Long]*。
func eraFreeBackPayload(result uint8, rewards []protocol.EraRebirthReward) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(result)
	w.WriteUint16(uint16(len(rewards)))
	for _, reward := range rewards {
		w.WriteInt32(reward.ItemID)
		w.WriteInt64(reward.Num)
	}
	return w.Bytes()
}

// eraPanelPayload 构造 852/1 面板最小回包，用于单测免费快速挂机次数。
//
// 线格式: [Int level][UInt16 pairCount][pairCount×(Int,Int)][Int×12]，
// 尾部 [10] = 已用免费次数，[11] = 已用付费次数。
func eraPanelPayload(freeUsed, paidUsed int32) []byte {
	w := protocol.NewWriter()
	w.WriteInt32(4)
	w.WriteUint16(0)
	tail := make([]int32, 12)
	tail[5] = 172800
	tail[10] = freeUsed
	tail[11] = paidUsed
	for _, v := range tail {
		w.WriteInt32(v)
	}
	return w.Bytes()
}

// respondRebirthQuickAutoAnchors 为 2026-11-21 新增的
// 852/1 面板 → 852/9 收益 → 701/0 锚点 → 852/8 快速挂机链路提供默认回包，
// 避免旧用例等待 6 秒超时。需要自定义行为的用例可自行覆盖这些动作。
func respondRebirthQuickAutoAnchors(conn net.Conn, pkt *protocol.Packet) bool {
	switch pkt.ActionID {
	case protocol.ActionIDEraRebirthLandPanel:
		_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
			eraPanelPayload(protocol.EraRebirthQuickFreeDailyLimit, 0)))
		return true
	case protocol.ActionIDEraRebirthLandAutoProfit:
		w := protocol.NewWriter()
		w.WriteUint8(protocol.EraRebirthResultNoProfit)
		w.WriteInt32(0)
		w.WriteUint16(0)
		_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		return true
	case protocol.ActionIDStateRecordGetState:
		w := protocol.NewWriter()
		w.WriteInt32(8)
		w.WriteInt32(0)
		_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		return true
	case protocol.ActionIDEraRebirthLandQuickAuto:
		_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
			eraFreeBackPayload(protocol.EraRebirthResultNoTimes, nil)))
		return true
	}
	return false
}

func TestRebirthLandRoutine_ClaimsFreeReward(t *testing.T) {
	var freeAwardCalls, freeBackCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if respondRebirthQuickAutoAnchors(conn, pkt) {
			return
		}
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(int32(i))
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			freeAwardCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultSuccess}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			freeBackCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := freeAwardCalls.Load(); got != 1 {
		t.Fatalf("渡业仙池免费奖励 calls = %d, want 1", got)
	}
	if got := freeBackCalls.Load(); got != 0 {
		t.Fatalf("免费奖励成功后不应再调 852/6, 实际 %d 次", got)
	}
}

func TestRebirthLandRoutine_SkipsWhenTaskComplete(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if respondRebirthQuickAutoAnchors(conn, pkt) {
			return
		}
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
		case protocol.ActionIDEraRebirthLandFreeBack:
			claimCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := claimCalls.Load(); got != 0 {
		t.Fatalf("claim calls = %d, want 0", got)
	}
}

func TestRebirthLandRoutine_NoGiftStops(t *testing.T) {
	var freeBackCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if respondRebirthQuickAutoAnchors(conn, pkt) {
			return
		}
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
		case protocol.ActionIDEraRebirthLandFreeAward, protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			freeBackCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthFreeBackResultPacket(
				protocol.EraRebirthResultNoFreeStorage, nil))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := freeBackCalls.Load(); got != 1 {
		t.Fatalf("仙池无奖励时应兜底调用 852/6 一次, 实际 %d 次", got)
	}
}

// 登录后置钩子：面板 + 一次成功抽取，随后渡业符不足自动停止；元宝不变。
func TestRunRebirthAltarOnLogin_DrawsUntilInsufficient(t *testing.T) {
	if !altarDrawEnabled {
		t.Skip("渡业台自动抽取已默认停用，登录钩子不再发抽取")
	}
	var drawCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if respondRebirthQuickAutoAnchors(conn, pkt) {
			return
		}
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			n := drawCalls.Add(1)
			result := protocol.EraRebirthResultSuccess
			if n > 1 {
				result = protocol.EraRebirthResultNotEnoughFu
			}
			w := protocol.NewWriter()
			w.WriteUint8(result)
			w.WriteInt32(1)
			w.WriteInt32(4)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		}
	})
	defer sess.Close()

	beforeIngots := sess.GetPlayerState().Ingots
	RunRebirthAltarOnLogin(context.Background(), sess)
	if got := drawCalls.Load(); got != 2 {
		t.Fatalf("抽取调用 = %d, want 2 (1 次成功 + 1 次渡业符不足)", got)
	}
	// 第二次登录钩子调用应被去重，不再发抽取。
	RunRebirthAltarOnLogin(context.Background(), sess)
	if got := drawCalls.Load(); got != 2 {
		t.Fatalf("重复钩子不应再次抽取, calls = %d", got)
	}
	if ingots := sess.GetPlayerState().Ingots; ingots != beforeIngots {
		t.Fatalf("元宝发生变化: before=%d after=%d", beforeIngots, ingots)
	}
}

// TestRebirthLandRoutine_TodayAwardPrevActFallback 验证 852/14 裸载荷被静默
// 丢弃后，使用服务端最后受理动作号重试一次。
func TestRebirthLandRoutine_TodayAwardPrevActFallback(t *testing.T) {
	var todayBare, todayPrev atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if respondRebirthQuickAutoAnchors(conn, pkt) {
			return
		}
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 0, MaxTimes: 1, PerActiveAdd: 20},
			}))
		case protocol.ActionIDEraRebirthLandPoolPanel:
			w := protocol.NewWriter()
			for i := 0; i < 6; i++ {
				w.WriteInt32(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandDraw:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraDrawPayload(protocol.EraRebirthResultNotEnoughFu)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultAlreadyGet}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			if len(pkt.Payload) == 0 {
				todayBare.Add(1)
				return // 模拟网关静默丢弃裸载荷
			}
			todayPrev.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDEraRebirthLandFreeBack:
			_ = protocol.WritePacket(conn, protocol.BuildEraRebirthFreeBackResultPacket(
				protocol.EraRebirthResultNoFreeStorage, nil))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if todayBare.Load() != 1 || todayPrev.Load() != 1 {
		t.Fatalf("852/14 应先裸载荷后 PrevAct 重试, bare=%d prev=%d", todayBare.Load(), todayPrev.Load())
	}
}

// eraProfitPayload 构造 852/9 回包 [UByte result][Int profit][UInt16 count][Int,Long]*。
func eraProfitPayload(result uint8, profit int32, rewards []protocol.EraRebirthReward) []byte {
	w := protocol.NewWriter()
	w.WriteUint8(result)
	w.WriteInt32(profit)
	w.WriteUint16(uint16(len(rewards)))
	for _, reward := range rewards {
		w.WriteInt32(reward.ItemID)
		w.WriteInt64(reward.Num)
	}
	return w.Bytes()
}

// 抓包链路 (2026-11-21): 852/9 领取挂机收益后，官方客户端连续两次 852/8 免费快速挂机。
func TestRebirthLandRoutine_QuickAutoTwice(t *testing.T) {
	var profitCalls, quickCalls, panelCalls atomic.Int32
	reward := []protocol.EraRebirthReward{{ItemID: 10992, Num: 285120}}
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPanel:
			// 初始面板已用 0 次；每次快速挂机成功后刷新为 1、2。
			used := panelCalls.Add(1) - 1
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraPanelPayload(used, 0)))
		case protocol.ActionIDEraRebirthLandAutoProfit:
			profitCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraProfitPayload(protocol.EraRebirthResultSuccess, 87105, reward)))
		case protocol.ActionIDEraRebirthLandQuickAuto:
			quickCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraFreeBackPayload(protocol.EraRebirthResultSuccess, reward)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultAlreadyGet}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := profitCalls.Load(); got != 1 {
		t.Fatalf("852/9 调用 = %d, want 1", got)
	}
	if got := quickCalls.Load(); got != 2 {
		t.Fatalf("852/8 免费快速挂机调用 = %d, want 2", got)
	}
}

// 失败码分支: 852/8 返回 NO_TIMES(9) 后立即停止，不再补第二次，也不阻断后续日常。
func TestRebirthLandRoutine_QuickAutoNoTimesStops(t *testing.T) {
	var quickCalls, assistantCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraPanelPayload(0, 0)))
		case protocol.ActionIDEraRebirthLandAutoProfit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraProfitPayload(protocol.EraRebirthResultSuccess, 1, nil)))
		case protocol.ActionIDStateRecordGetState:
			w := protocol.NewWriter()
			w.WriteInt32(8)
			w.WriteInt32(0)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDEraRebirthLandQuickAuto:
			quickCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraFreeBackPayload(protocol.EraRebirthResultNoTimes, nil)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultAlreadyGet}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDGameAssistantInfo:
			assistantCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := quickCalls.Load(); got != 1 {
		t.Fatalf("NO_TIMES 后应停止, 852/8 调用 = %d, want 1", got)
	}
	if got := assistantCalls.Load(); got != 1 {
		t.Fatalf("快速挂机失败不应阻断后续日常, 活跃度查询 = %d, want 1", got)
	}
}

// 852/9 回包为 PACKAGE_FULL(5) 时应熔断为背包已满，且不再触发快速挂机。
func TestRebirthLandRoutine_QuickAutoProfitBagFull(t *testing.T) {
	var quickCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraPanelPayload(0, 0)))
		case protocol.ActionIDEraRebirthLandAutoProfit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraProfitPayload(protocol.EraRebirthResultPackageFull, 0, nil)))
		case protocol.ActionIDEraRebirthLandQuickAuto:
			quickCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil)
	if err != scheduler.ErrBagFull {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
	if got := quickCalls.Load(); got != 0 {
		t.Fatalf("背包已满时不应触发 852/8, 实际 %d 次", got)
	}
}

// 关键回归: 面板显示免费次数已用尽 (quick_free_used=2) 时，852/8 绝不调用。
// 线上实测第 3 次 852/8 会扣 10 元宝且仍返回 result=0。
func TestRebirthLandRoutine_QuickAutoSkipsWhenFreeUsedUp(t *testing.T) {
	var quickCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraPanelPayload(protocol.EraRebirthQuickFreeDailyLimit, 0)))
		case protocol.ActionIDEraRebirthLandAutoProfit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraProfitPayload(protocol.EraRebirthResultSuccess, 1, nil)))
		case protocol.ActionIDEraRebirthLandQuickAuto:
			quickCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraFreeBackPayload(protocol.EraRebirthResultSuccess, nil)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultAlreadyGet}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := quickCalls.Load(); got != 0 {
		t.Fatalf("免费次数已用尽仍调用 852/8 %d 次，存在扣元宝风险", got)
	}
}

// 面板 quick_free_used=1 时只补 1 次，结束后免费次数到 2 即停止。
func TestRebirthLandRoutine_QuickAutoOnlyRemaining(t *testing.T) {
	var quickCalls, panelCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPanel:
			used := int32(1)
			if n := panelCalls.Add(1); n > 1 {
				used = 2
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraPanelPayload(used, 0)))
		case protocol.ActionIDEraRebirthLandAutoProfit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraProfitPayload(protocol.EraRebirthResultSuccess, 1, nil)))
		case protocol.ActionIDEraRebirthLandQuickAuto:
			quickCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraFreeBackPayload(protocol.EraRebirthResultSuccess, nil)))
		case protocol.ActionIDEraRebirthLandFreeAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultAlreadyGet}))
		case protocol.ActionIDEraRebirthLandTodayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.EraRebirthResultNoCanGetGift}))
		case protocol.ActionIDGameAssistantInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGameAssistantInfoResultPacket([]protocol.DailyActivityTask{
				{TaskID: DailyTaskRebirthLand, CurTimes: 1, MaxTimes: 1, PerActiveAdd: 20},
			}))
		}
	})
	defer sess.Close()

	if err := NewRebirthLandRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rebirth land: %v", err)
	}
	if got := quickCalls.Load(); got != 1 {
		t.Fatalf("剩余 1 次时 852/8 调用 = %d, want 1", got)
	}
}

// TestRebirthLandRoutine_QuickAutoSilentDropBacksOff 验证 852/8 被静默丢弃时
// 按连续丢包退避，而不是 return nil 等到下一个 cron 时段。
func TestRebirthLandRoutine_QuickAutoSilentDropBacksOff(t *testing.T) {
	reward := []protocol.EraRebirthReward{{ItemID: 10992, Num: 285120}}
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDEraRebirthLandPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraPanelPayload(0, 0)))
		case protocol.ActionIDEraRebirthLandAutoProfit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				eraProfitPayload(protocol.EraRebirthResultSuccess, 87105, reward)))
		case protocol.ActionIDEraRebirthLandQuickAuto:
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := NewRebirthLandRoutine().Execute(ctx, sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("快速挂机无回包应返回 CooldownError, got %v", err)
	}
	if got := scheduler.SilentDropCount(sess, "rebirth_land_quick"); got < 1 {
		t.Fatalf("应记录轮回净地快速挂机静默丢包，got %d", got)
	}
}
