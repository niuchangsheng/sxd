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

func TestXiuZhenRealmAwardRoutine_Success(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDXiuZhenRealmMain:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDXiuZhenRealmExtraAward:
			claimCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildXiuZhenRealmResultPacket(
				protocol.XiuZhenRealmSuccess, []protocol.XiuZhenRealmReward{
					{ItemID: 8808, Num: 5},
					{ItemID: 5852, Num: 1},
				}))
		}
	})
	defer sess.Close()

	// 领取成功后 2 小时再检查（奖励可累积重复领取）。
	err := NewXiuZhenRealmAwardRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("领取成功应返回 2 小时冷却, got %v", err)
	}
	if d := time.Until(cd.Until); d < 90*time.Minute || d > 130*time.Minute {
		t.Fatalf("冷却时长 = %v, want ~2h", d)
	}
	if claimCalls.Load() != 1 {
		t.Fatalf("应领取一次, calls = %d", claimCalls.Load())
	}
}

func TestXiuZhenRealmAwardRoutine_MainSilentStillClaims(t *testing.T) {
	var claimCalls atomic.Int32
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDXiuZhenRealmExtraAward {
			claimCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildXiuZhenRealmResultPacket(
				protocol.XiuZhenRealmSuccess, nil))
		}
		// 552/1 静默丢弃：不回包。
	})
	defer sess.Close()

	err := NewXiuZhenRealmAwardRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("主面板无回包不应阻断领取, got %v", err)
	}
	if claimCalls.Load() != 1 {
		t.Fatalf("主面板无回包时仍应尝试领取, calls = %d", claimCalls.Load())
	}
}

func TestXiuZhenRealmAwardRoutine_ResultCodes(t *testing.T) {
	cases := []struct {
		name   string
		code   uint8
		check  func(error) bool
		reason string
	}{
		{
			name: "已领取进冷却",
			code: protocol.XiuZhenRealmIsGet,
			check: func(err error) bool {
				var cd *scheduler.CooldownError
				return errors.As(err, &cd) && cd.Until.After(time.Now().Add(30*time.Minute))
			},
		},
		{
			name: "背包已满",
			code: protocol.XiuZhenRealmItemPackFull,
			check: func(err error) bool {
				return errors.Is(err, scheduler.ErrBagFull)
			},
		},
		{
			name: "暂无可领取奖励",
			code: protocol.XiuZhenRealmNoAward,
			check: func(err error) bool {
				var cd *scheduler.CooldownError
				return errors.As(err, &cd)
			},
		},
		{
			name: "条件未满足",
			code: protocol.XiuZhenRealmCondLimit,
			check: func(err error) bool {
				var cd *scheduler.CooldownError
				return errors.As(err, &cd)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
				switch pkt.ActionID {
				case protocol.ActionIDXiuZhenRealmMain:
					_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
				case protocol.ActionIDXiuZhenRealmExtraAward:
					_ = protocol.WritePacket(conn, protocol.BuildXiuZhenRealmResultPacket(tc.code, nil))
				}
			})
			defer sess.Close()

			err := NewXiuZhenRealmAwardRoutine().Execute(context.Background(), sess, nil)
			if err == nil || !tc.check(err) {
				t.Fatalf("结果码 %d 应返回预期错误, got %v", tc.code, err)
			}
		})
	}
}

func TestXiuZhenRealmAwardRoutine_ClaimDroppedBacksOff(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDXiuZhenRealmMain {
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
		// 552/5 静默丢弃。
	})
	defer sess.Close()

	err := NewXiuZhenRealmAwardRoutine().Execute(context.Background(), sess, nil)
	if err == nil {
		t.Fatal("领取无回包应返回错误")
	}
	if n := scheduler.SilentDropCount(sess, "xiuzhen_realm_award"); n < 1 {
		t.Fatalf("应记录静默丢包, got %d", n)
	}
}
