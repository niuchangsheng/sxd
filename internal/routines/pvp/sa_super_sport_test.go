package pvp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/kit"
	"sxd-pie-ng/internal/scheduler"
)

// saSuperSportInfo 构造带指定剩余次数与两名对手的面板。
func saSuperSportInfo(left int32) *protocol.Packet {
	return protocol.BuildSaSuperSportRaceInfoResultPacket(&protocol.SaSuperSportRaceInfo{
		RaceState: protocol.SaSuperSportRaceScore,
		LeftTimes: left,
		Score:     100,
		Rank:      30,
		Opponents: []protocol.SaSuperSportOpponent{
			{PlayerID: 1, Slot: 5, Platform: "x", Server: "s0", Nickname: "强者",
				WarPower: 900, BattleID: 501, Level: 300},
			{PlayerID: 2, Slot: 4, Platform: "x", Server: "s0", Nickname: "弱者",
				WarPower: 100, BattleID: 401, Level: 300},
		},
	})
}

func TestSaSuperSportRoutine_ChallengesUntilUsedUp(t *testing.T) {
	stubSaintAreaSession(t)
	var infoCalls, challengeCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaSuperSportRaceInfo:
			n := infoCalls.Add(1)
			switch n {
			case 1:
				_ = protocol.WritePacket(conn, saSuperSportInfo(2))
			case 2:
				_ = protocol.WritePacket(conn, saSuperSportInfo(1))
			default:
				_ = protocol.WritePacket(conn, saSuperSportInfo(0))
			}
		case protocol.ActionIDSaSuperSportChallenge:
			challengeCalls.Add(1)
			// 挑战请求应为 [Byte slot=4][Int battle_id=401]。
			if len(pkt.Payload) < 5 || pkt.Payload[0] != 4 ||
				int32(pkt.Payload[1])<<24|int32(pkt.Payload[2])<<16|
					int32(pkt.Payload[3])<<8|int32(pkt.Payload[4]) != 401 {
				t.Errorf("challenge 载荷不符: %X", pkt.Payload)
			}
			_ = protocol.WritePacket(conn, protocol.BuildSaSuperSportChallengeResultPacket(
				protocol.SaSuperSportResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewSaSuperSportRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa super sport: %v", err)
	}
	if got := challengeCalls.Load(); got != 2 {
		t.Fatalf("剩余 2 次时应挑战 2 次, got %d", got)
	}
	if got := infoCalls.Load(); got < 3 {
		t.Fatalf("每次挑战后应刷新面板, info calls=%d", got)
	}
}

func TestSaSuperSportRoutine_NoChallengeTimesStops(t *testing.T) {
	stubSaintAreaSession(t)
	var challengeCalls atomic.Int32
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaSuperSportRaceInfo:
			_ = protocol.WritePacket(conn, saSuperSportInfo(3))
		case protocol.ActionIDSaSuperSportChallenge:
			challengeCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildSaSuperSportChallengeResultPacket(
				protocol.SaSuperSportResultNoChallengeTimes))
		}
	})
	defer sess.Close()

	if err := NewSaSuperSportRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("sa super sport: %v", err)
	}
	if got := challengeCalls.Load(); got != 1 {
		t.Fatalf("次数不足后应立即停止, challenges=%d", got)
	}
}

func TestSaSuperSportRoutine_NoShengHunCooldown(t *testing.T) {
	stubSaintAreaSession(t)
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDSaSuperSportRaceInfo:
			_ = protocol.WritePacket(conn, saSuperSportInfo(3))
		case protocol.ActionIDSaSuperSportChallenge:
			_ = protocol.WritePacket(conn, protocol.BuildSaSuperSportChallengeResultPacket(
				protocol.SaSuperSportResultNoShengHun))
		}
	})
	defer sess.Close()

	err := NewSaSuperSportRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("圣魂不足应返回 CooldownError, got %v", err)
	}
}

func TestSaSuperSportRoutine_PanelSilentDropBacksOff(t *testing.T) {
	stubSaintAreaSession(t)
	sess := newPvPMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID == protocol.ActionIDSaSuperSportRaceInfo {
			// 静默丢弃：不回包。
		}
	})
	defer sess.Close()

	ctx := kit.WithTimeoutOverride(context.Background(), 150*time.Millisecond)
	err := NewSaSuperSportRoutine().Execute(ctx, sess, nil)
	if err == nil {
		t.Fatal("面板无回包应返回退避错误")
	}
}

func TestPickSaSuperSportOpponent_LowestWarPower(t *testing.T) {
	opponents := []protocol.SaSuperSportOpponent{
		{Slot: 5, BattleID: 1, WarPower: 900},
		{Slot: 4, BattleID: 2, WarPower: 100},
		{Slot: 3, BattleID: 0, WarPower: 1}, // 无效记录
		{Slot: 0, BattleID: 3, WarPower: 1}, // 无效记录
	}
	got, ok := pickSaSuperSportOpponent(opponents)
	if !ok || got.Slot != 4 || got.BattleID != 2 {
		t.Fatalf("pick = %+v ok=%v", got, ok)
	}
	if _, ok := pickSaSuperSportOpponent(nil); ok {
		t.Fatal("空列表应返回 false")
	}
}
