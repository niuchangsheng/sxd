package dungeon

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/dictionary"
	"sxd-pie-ng/internal/protocol"
)

// TestChallengeStoryMission_OpenBoxFailureDoesNotBookClear 验证 open_box 回包为失败码时，
// 不得继续拾取奖励，也不得把本关记入已通关列表/本地收益（避免「任务显示通关但资源没变化」）。
func TestChallengeStoryMission_OpenBoxFailureDoesNotBookClear(t *testing.T) {
	var pickupCalls atomic.Int32
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		switch pkt.ActionID {
		case protocol.ActionIDMissionPlotEnter:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.MissionResultSuccess}))
		case protocol.ActionIDMissionFightMonster:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x00, protocol.MissionResultSuccess}))
		case protocol.ActionIDMissionRank:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x00}))
		case protocol.ActionIDMissionOpenBox:
			// 宝箱开启失败（非 0）。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x01}))
		case protocol.ActionIDMissionPickupAward:
			pickupCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x00}))
		}
		return true
	})
	sess.SetStamina(50)

	mission := &dictionary.Mission{ID: 88, Name: "测试关卡", Type: 1, Monsters: "1"}
	costStamina, gainExp, gainCoins := 0, int64(0), int64(0)
	cleared := map[int]bool{}

	ok := challengeStoryMission(context.Background(), sess, mission,
		func() bool { return true }, &costStamina, &gainExp, &gainCoins, cleared)
	if ok {
		t.Fatal("宝箱开启失败时不应判定关卡通关")
	}
	if pickupCalls.Load() != 0 {
		t.Fatalf("宝箱开启失败后不应拾取奖励, pickup calls = %d", pickupCalls.Load())
	}
	if costStamina != 0 || gainExp != 0 || gainCoins != 0 {
		t.Fatalf("失败时不应记账: stamina=%d exp=%d coins=%d", costStamina, gainExp, gainCoins)
	}
	if cleared[mission.ID] {
		t.Fatal("失败时不应标记关卡已通关")
	}
}

// TestXunXianRoutine_StopsAfterConsecutiveFailures 验证寻仙之路节点奖励连续失败 3 次后
// 结束本轮（挑战副本 N 次失败退出），并仍然执行一键领取。
func TestXunXianRoutine_StopsAfterConsecutiveFailures(t *testing.T) {
	var awardCalls, oneClickCalls atomic.Int32
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		switch pkt.ActionID {
		case protocol.ActionIDXunXianGetProgress:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x00, 0x00}))
		case protocol.ActionIDXunXianChallenge:
			awardCalls.Add(1)
			// 失败码 4=FAILED。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.XunXianResultFailed}))
		case protocol.ActionIDXunXianOneClickAward:
			oneClickCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.XunXianResultSuccess}))
		}
		return true
	})

	if err := NewXunXianRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("xunxian: %v", err)
	}
	if got := awardCalls.Load(); got != 3 {
		t.Fatalf("连续 3 次失败后应停止, award calls = %d", got)
	}
	if got := oneClickCalls.Load(); got != 1 {
		t.Fatalf("应执行一次一键领取, calls = %d", got)
	}
}
