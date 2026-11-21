// Package minigame 提供吉星高照、仙履奇缘答题、刮刮卡、钓鱼等益智趣味活动。
//
// @author Ateng
// @since 2026-11-02
package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestShouldRerollRollCake(t *testing.T) {
	cases := []struct {
		name       string
		dice       []int8
		isLastRoll bool
		want       bool
	}{
		{"两吉非最后", []int8{4, 4, 1, 2, 3, 5}, false, true},
		{"三吉非最后", []int8{1, 2, 3, 4, 4, 4}, false, true},
		{"一吉非最后", []int8{4, 1, 2, 3, 5, 6}, false, false},
		{"零吉非最后", []int8{1, 2, 3, 5, 6, 1}, false, false},
		{"六吉非最后", []int8{4, 4, 4, 4, 4, 4}, false, true},
		{"一吉但今日最后一次", []int8{4, 1, 2, 3, 5, 6}, true, true},
		{"零吉但今日最后一次", []int8{1, 2, 3, 5, 6, 1}, true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRerollRollCake(tc.dice, tc.isLastRoll); got != tc.want {
				t.Errorf("shouldRerollRollCake(%v, last=%v) = %v, want %v", tc.dice, tc.isLastRoll, got, tc.want)
			}
		})
	}
}

func TestCountRollCakeJi(t *testing.T) {
	cases := []struct {
		dice []int8
		want int
	}{
		{[]int8{4, 4, 4, 1, 2, 3}, 3},
		{[]int8{1, 2, 3, 5, 6, 1}, 0},
		{[]int8{4, 4, 4, 4, 4, 4}, 6},
		{nil, 0},
	}

	for _, tc := range cases {
		if got := countRollCakeJi(tc.dice); got != tc.want {
			t.Errorf("countRollCakeJi(%v) = %d, want %d", tc.dice, got, tc.want)
		}
	}
}

func TestAllRollCakeJi(t *testing.T) {
	if !allRollCakeJi([]int8{4, 4, 4, 4, 4, 4}) {
		t.Error("六吉应判定为全吉")
	}
	if allRollCakeJi([]int8{4, 4, 4, 4, 4, 3}) {
		t.Error("五吉一非吉不应判定为全吉")
	}
	if allRollCakeJi(nil) {
		t.Error("空骰子不应判定为全吉")
	}
}

// TestRollCakeRoutine_AwardFailureStops 验证补领回包结果码非成功时，
// 不得继续查询次数/投掷（否则会掩盖「奖励没到账却继续推进」）。
func TestRollCakeRoutine_AwardFailureStops(t *testing.T) {
	var countCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDRollCakeGetState:
			w := protocol.NewWriter()
			w.WriteUint8(protocol.RollCakeCodeHaveRecord)
			w.WriteInt8(3)  // type
			w.WriteInt32(0) // reroll_ingot
			for i := 0; i < 6; i++ {
				w.WriteInt8(4)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDRollCakeGetAward:
			// 回包到达但领奖失败。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.RollCakeCodeFailure}))
		case protocol.ActionIDRollCakeGetCount:
			countCalls.Add(1)
		}
	})

	if err := NewLuckyStarRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("rollcake: %v", err)
	}
	if got := countCalls.Load(); got != 0 {
		t.Fatalf("补领失败后不应继续查询次数, count calls = %d", got)
	}
}
