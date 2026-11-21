package dungeon

import (
	"context"
	"net"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
)

// TestPracticeMission_QuickSilentDropDoesNotBookRewards 验证扫荡加速请求被静默丢弃时
// 不得扣体力/加奖励（旧实现丢弃回包后无条件记账）。
func TestPracticeMission_QuickSilentDropDoesNotBookRewards(t *testing.T) {
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		switch pkt.ActionID {
		case protocol.ActionIDPracticeStart:
			res, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
				Result:    protocol.PracticeResultSuccess,
				MissionID: 43,
				Count:     1,
			})
			_ = protocol.WritePacket(conn, res)
		case protocol.ActionIDPracticeQuickly:
			// 静默丢弃：不回包。
		}
		return true
	})
	sess.SetStamina(50)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cost, exp, coins := 0, int64(0), int64(0)
	ok, rounds, err := practiceMission(ctx, sess, 43, 1, func() bool { return true },
		&cost, &exp, &coins)
	if ok || rounds != 0 {
		t.Fatalf("加速无回包时不应判定扫荡成功: ok=%v rounds=%d", ok, rounds)
	}
	if err == nil {
		t.Fatal("加速无回包应返回错误")
	}
	if cost != 0 || exp != 0 || coins != 0 {
		t.Fatalf("加速无回包时不应记账: stamina=%d exp=%d coins=%d", cost, exp, coins)
	}
	if got := sess.GetBaseStamina(); got != 50 {
		t.Fatalf("加速无回包时不应扣体力, stamina=%d", got)
	}
}

// TestPracticeMission_QuickSuccessBooksRewards 验证加速回包成功时才记账。
func TestPracticeMission_QuickSuccessBooksRewards(t *testing.T) {
	sess := newTownResetTestSession(t, func(pkt *protocol.Packet, conn net.Conn) bool {
		switch pkt.ActionID {
		case protocol.ActionIDPracticeStart:
			res, _ := protocol.BuildStartPracticeResultPacket(protocol.StartPracticeResult{
				Result:    protocol.PracticeResultSuccess,
				MissionID: 43,
				Count:     1,
			})
			_ = protocol.WritePacket(conn, res)
		case protocol.ActionIDPracticeQuickly:
			res, _ := protocol.BuildQuicklyResultPacket(protocol.QuicklyResult{
				Result: protocol.PracticeResultSuccess,
				Count:  1,
			})
			_ = protocol.WritePacket(conn, res)
		}
		return true
	})
	sess.SetStamina(50)

	cost, exp, coins := 0, int64(0), int64(0)
	ok, rounds, err := practiceMission(context.Background(), sess, 43, 1,
		func() bool { return true }, &cost, &exp, &coins)
	if err != nil || !ok || rounds != 1 {
		t.Fatalf("加速成功应完成 1 轮: ok=%v rounds=%d err=%v", ok, rounds, err)
	}
	if cost != 5 || exp != 2500 || coins != 12000 {
		t.Fatalf("记账不符: stamina=%d exp=%d coins=%d", cost, exp, coins)
	}
	if got := sess.GetBaseStamina(); got != 45 {
		t.Fatalf("应扣除 5 点体力, stamina=%d", got)
	}
}
