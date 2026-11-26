package dungeon

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"
	"time"

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

// TestChallengeStoryMission_PickupAlreadyPickedBooksClearWithoutRewards 验证重复挑战
// 已通关副本时 pickup_award 返回 23（奖励已领取）：应记为已通关并消耗体力，
// 但不再重复累加本地经验/铜钱收益。
func TestChallengeStoryMission_PickupAlreadyPickedBooksClearWithoutRewards(t *testing.T) {
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
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x00}))
		case protocol.ActionIDMissionPickupAward:
			pickupCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.MissionResultAwardAlreadyPicked}))
		}
		return true
	})
	sess.SetStamina(50)

	mission := &dictionary.Mission{ID: 88, Name: "重复挑战关卡", Type: 1, Monsters: "1"}
	costStamina, gainExp, gainCoins := 0, int64(0), int64(0)
	cleared := map[int]bool{}

	ok := challengeStoryMission(context.Background(), sess, mission,
		func() bool { return true }, &costStamina, &gainExp, &gainCoins, cleared)
	if !ok {
		t.Fatal("pickup=23（已领取）应判定关卡通关")
	}
	if pickupCalls.Load() != 1 {
		t.Fatalf("应拾取一次, calls = %d", pickupCalls.Load())
	}
	if !cleared[mission.ID] {
		t.Fatal("应标记关卡已通关")
	}
	if costStamina != 5 {
		t.Fatalf("应消耗 5 点体力, got %d", costStamina)
	}
	if gainExp != 0 || gainCoins != 0 {
		t.Fatalf("重复挑战无新增奖励时不应累加本地收益: exp=%d coins=%d", gainExp, gainCoins)
	}
	if got := sess.GetBaseStamina(); got != 45 {
		t.Fatalf("体力应扣减 5, got %d", got)
	}
}

// TestChallengeStoryMission_EnterTimeoutMarksContextBad 验证剧情副本入口网络超时
// 会标记 Mod 4 上下文失效，由 quest_cycle 统一重连，而不是逐关卡盲打。
func TestChallengeStoryMission_EnterTimeoutMarksContextBad(t *testing.T) {
	old := storyMissionCallTimeout
	storyMissionCallTimeout = 120 * time.Millisecond
	defer func() { storyMissionCallTimeout = old }()

	// 服务端对 enter_mission 完全无回包。
	sess := newTownResetTestSession(t, func(*protocol.Packet, net.Conn) bool { return false })
	questContextBad.Delete(sess)

	mission := &dictionary.Mission{ID: 990001, Type: 1, Name: "测试精英关", Monsters: "1493"}
	var cost int
	var exp, coins int64
	ok := challengeStoryMission(context.Background(), sess, mission,
		func() bool { return true }, &cost, &exp, &coins, map[int]bool{})
	if ok {
		t.Fatal("入口超时不应返回成功")
	}
	if _, bad := questContextBad.Load(sess); !bad {
		t.Fatal("入口超时后应标记 questContextBad，触发会话重连而不是继续盲打")
	}
}

// TestChallengeStoryMission_FightTimeoutMarksContextBad 验证 fight_monster 超时
// 同样标记 Mod 4 上下文失效。
func TestChallengeStoryMission_FightTimeoutMarksContextBad(t *testing.T) {
	old := storyMissionCallTimeout
	storyMissionCallTimeout = 120 * time.Millisecond
	defer func() { storyMissionCallTimeout = old }()

	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		if pkt.ActionID != protocol.ActionIDMissionPlotEnter {
			return false
		}
		payload := make([]byte, 24)
		payload[0] = protocol.MissionResultSuccess
		binary.BigEndian.PutUint16(payload[18:20], 1)
		binary.BigEndian.PutUint32(payload[20:24], 1493)
		_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, payload))
		return true
	})
	questContextBad.Delete(sess)

	mission := &dictionary.Mission{ID: 990002, Type: 1, Name: "测试精英关", Monsters: "1493"}
	var cost int
	var exp, coins int64
	ok := challengeStoryMission(context.Background(), sess, mission,
		func() bool { return true }, &cost, &exp, &coins, map[int]bool{})
	if ok {
		t.Fatal("fight 超时不应返回成功")
	}
	if _, bad := questContextBad.Load(sess); !bad {
		t.Fatal("fight 超时后应标记 questContextBad")
	}
}
