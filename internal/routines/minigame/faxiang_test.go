package minigame

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

func faxiangFoInfoPayload(level, exp int32, awards []int32, curAward, finish int32) []byte {
	w := protocol.NewWriter().WriteInt32(level).WriteInt32(exp)
	w.WriteUint16(uint16(len(awards)))
	for _, id := range awards {
		w.WriteInt32(id)
	}
	w.WriteInt32(curAward).WriteInt32(finish)
	return w.Bytes()
}

func faxiangMoInfoPayload(level, exp, next, stunt, supernatural, pattern int32) []byte {
	return protocol.NewWriter().
		WriteInt32(level).WriteInt32(exp).WriteInt32(next).
		WriteInt32(stunt).WriteInt32(supernatural).WriteInt32(pattern).
		Bytes()
}

func faxiangPrayPayload(ids ...int32) []byte {
	w := protocol.NewWriter().WriteUint8(protocol.FaxiangSuccess)
	w.WriteUint16(uint16(len(ids)))
	for _, id := range ids {
		w.WriteInt32(id)
	}
	return w.Bytes()
}

func TestFaxiangRoutine_ClaimsPraysAndChallenges(t *testing.T) {
	var claimCalls, prayCalls, chooseCalls, challengeCalls atomic.Int32
	var chosenAward atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangGetPanelInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFaxiangExtraFoInfo:
			// 已有一笔炼化完成待领取。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangFoInfoPayload(3, 1604, nil, 14, 1780000000)))
		case protocol.ActionIDFaxiangGainPrayAward:
			claimCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangSuccess}))
		case protocol.ActionIDFaxiangExtraPray:
			prayCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, faxiangPrayPayload(5, 15, 14)))
		case protocol.ActionIDFaxiangExtraChoosePrayAward:
			chooseCalls.Add(1)
			if len(pkt.Payload) >= 4 {
				chosenAward.Store(int32(pkt.Payload[0])<<24 | int32(pkt.Payload[1])<<16 |
					int32(pkt.Payload[2])<<8 | int32(pkt.Payload[3]))
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				protocol.NewWriter().WriteUint8(protocol.FaxiangSuccess).WriteInt32(1780000600).Bytes()))
		case protocol.ActionIDFaxiangExtraMoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangMoInfoPayload(1, 0, 0, 0, 282, 97)))
		case protocol.ActionIDFaxiangExtraMoChallenge:
			challengeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				protocol.NewWriter().WriteUint8(protocol.FaxiangSuccess).WriteInt32(28).Bytes()))
		}
	})
	defer sess.Close()

	err := NewFaxiangRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("err = %v, want cooldown (炼化已开始，等待领取)", err)
	}
	if claimCalls.Load() != 1 || prayCalls.Load() != 1 || chooseCalls.Load() != 1 || challengeCalls.Load() != 1 {
		t.Fatalf("calls claim=%d pray=%d choose=%d challenge=%d, want 1/1/1/1",
			claimCalls.Load(), prayCalls.Load(), chooseCalls.Load(), challengeCalls.Load())
	}
	// [5,15,14] 中 id 15 的单位道缘熟练度最高 (760/100)，应被选中。
	if got := chosenAward.Load(); got != 15 {
		t.Fatalf("chosen award = %d, want 15", got)
	}
}

func TestFaxiangRoutine_RefineNotFinishedSkipsPray(t *testing.T) {
	var prayCalls, chooseCalls, challengeCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangGetPanelInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFaxiangExtraFoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangFoInfoPayload(3, 1604, nil, 14, 1780000000)))
		case protocol.ActionIDFaxiangGainPrayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangNotPrayAward}))
		case protocol.ActionIDFaxiangExtraPray:
			prayCalls.Add(1)
		case protocol.ActionIDFaxiangExtraChoosePrayAward:
			chooseCalls.Add(1)
		case protocol.ActionIDFaxiangExtraMoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangMoInfoPayload(1, 0, 0, 0, 282, 97)))
		case protocol.ActionIDFaxiangExtraMoChallenge:
			challengeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				protocol.NewWriter().WriteUint8(protocol.FaxiangSuccess).WriteInt32(28).Bytes()))
		}
	})
	defer sess.Close()

	err := NewFaxiangRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("err = %v, want cooldown", err)
	}
	if prayCalls.Load() != 0 || chooseCalls.Load() != 0 {
		t.Fatalf("炼化未完成时不应重新祈愿/炼化, pray=%d choose=%d", prayCalls.Load(), chooseCalls.Load())
	}
	// 守护魔兽与造物佛手相互独立，仍应尝试。
	if challengeCalls.Load() != 1 {
		t.Fatalf("challenge calls = %d, want 1", challengeCalls.Load())
	}
}

func TestFaxiangRoutine_ChooseNotEnoughDaoYuan(t *testing.T) {
	var chooseCalls, challengeCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangGetPanelInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFaxiangExtraFoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangFoInfoPayload(3, 1604, nil, 0, 0)))
		case protocol.ActionIDFaxiangExtraPray:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, faxiangPrayPayload(5, 15, 14)))
		case protocol.ActionIDFaxiangExtraChoosePrayAward:
			chooseCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangNotEnoughDaoYuan}))
		case protocol.ActionIDFaxiangExtraMoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangMoInfoPayload(1, 0, 0, 0, 282, 97)))
		case protocol.ActionIDFaxiangExtraMoChallenge:
			challengeCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewFaxiangRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("err = %v, want cooldown", err)
	}
	if chooseCalls.Load() != 1 {
		t.Fatalf("choose calls = %d, want 1", chooseCalls.Load())
	}
	// 佛/魔相互独立：道缘不足只暂停炼化，守护魔兽仍应尝试。
	if challengeCalls.Load() != 1 {
		t.Fatalf("challenge calls = %d, want 1", challengeCalls.Load())
	}
}

func TestFaxiangRoutine_GainPackageFull(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangGetPanelInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFaxiangExtraFoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangFoInfoPayload(3, 1604, nil, 14, 1780000000)))
		case protocol.ActionIDFaxiangGainPrayAward:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.FaxiangPackageFull}))
		}
	})
	defer sess.Close()

	err := NewFaxiangRoutine().Execute(context.Background(), sess, nil)
	if !errors.Is(err, scheduler.ErrBagFull) {
		t.Fatalf("err = %v, want ErrBagFull", err)
	}
}

func TestFaxiangRoutine_UsesPendingAwardList(t *testing.T) {
	var prayCalls atomic.Int32
	var chosenAward atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDFaxiangGetPanelInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0, 0, 0, 0}))
		case protocol.ActionIDFaxiangExtraFoInfo:
			// 上一轮祈愿后未选择：直接使用面板里的 award_list。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangFoInfoPayload(3, 1604, []int32{4, 12}, 0, 0)))
		case protocol.ActionIDFaxiangExtraPray:
			prayCalls.Add(1)
		case protocol.ActionIDFaxiangExtraChoosePrayAward:
			if len(pkt.Payload) >= 4 {
				chosenAward.Store(int32(pkt.Payload[0])<<24 | int32(pkt.Payload[1])<<16 |
					int32(pkt.Payload[2])<<8 | int32(pkt.Payload[3]))
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				protocol.NewWriter().WriteUint8(protocol.FaxiangSuccess).WriteInt32(1780000600).Bytes()))
		case protocol.ActionIDFaxiangExtraMoInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				faxiangMoInfoPayload(1, 0, 0, 0, 282, 97)))
		case protocol.ActionIDFaxiangExtraMoChallenge:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				protocol.NewWriter().WriteUint8(protocol.FaxiangHasChallenge).Bytes()))
		}
	})
	defer sess.Close()

	err := NewFaxiangRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("err = %v, want cooldown (炼化中)", err)
	}
	if prayCalls.Load() != 0 {
		t.Fatalf("已有待选奖励时不应重新祈愿, pray=%d", prayCalls.Load())
	}
	// 4: 120/400=0.30，12: 152/2000=0.076 → 应选 4。
	if got := chosenAward.Load(); got != 4 {
		t.Fatalf("chosen award = %d, want 4", got)
	}
}

func TestPickFaxiangPrayAward(t *testing.T) {
	if got := pickFaxiangPrayAward([]int32{5, 15, 14}); got != 15 {
		t.Fatalf("pick = %d, want 15", got)
	}
	if got := pickFaxiangPrayAward([]int32{4, 12}); got != 4 {
		t.Fatalf("pick = %d, want 4", got)
	}
	// 含未知奖励时仍优先已知奖励，全部未知才按原始顺序兜底。
	if got := pickFaxiangPrayAward([]int32{99, 4}); got != 4 {
		t.Fatalf("pick = %d, want 4", got)
	}
	if got := pickFaxiangPrayAward([]int32{99, 98}); got != 99 {
		t.Fatalf("全部未知应兜底首个, pick = %d, want 99", got)
	}
	// 12/16 的 cost_dao_yuan 分别为 2000/5000，均超过自动炼化上限。
	if got := pickFaxiangPrayAward([]int32{12, 16}); got != 0 {
		t.Fatalf("超出道缘上限应返回 0, pick = %d", got)
	}
	if got := pickFaxiangPrayAward(nil); got != 0 {
		t.Fatalf("empty pick = %d, want 0", got)
	}
}
