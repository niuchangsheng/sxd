package dungeon_test

import (
	"context"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/routines/dungeon"
)

// TestSpiritTrialRoutine_RetriesLossThenStopsMode 验证精灵试炼同一模式
// 连续战败 3 次后才停止，而不是首次失败就放弃。
func TestSpiritTrialRoutine_RetriesLossThenStopsMode(t *testing.T) {
	sess, srvConn := createTestSession(t, 5000)
	defer sess.Close()
	defer srvConn.Close()

	var challengeCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDStTrialsModeStatus:
			_ = protocol.WritePacket(srvConn, protocol.BuildStTrialsModeStatusResultPacket(
				[]protocol.StTrialsMode{{ModeType: 1, RemainChallengeTimes: 10}}))
		case protocol.ActionIDStTrialsModeInfo:
			_ = protocol.WritePacket(srvConn, protocol.BuildStTrialsModeInfoResultPacket(
				protocol.StTrialsModeInfo{Floor: 1, Pass: 0, AwardStatus: 1}))
		case protocol.ActionIDStTrialsGetAward:
			_ = protocol.WritePacket(srvConn, protocol.BuildStTrialsAwardResultPacket(
				protocol.StTrialsResultIsGet))
		case protocol.ActionIDStTrialsChallenge:
			challengeCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildStTrialsChallengeResultPacket(
				protocol.StTrialsResultFaild))
		default:
			return false
		}
		return true
	})

	if err := dungeon.NewSpiritTrialRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("精灵试炼失败重试流程异常: %v", err)
	}
	if got := challengeCalls.Load(); got != 3 {
		t.Fatalf("连续战败应重试到 3 次上限, 实际 %d 次", got)
	}
}

// TestZodiacRoutine_RetriesLossThenStops 验证生肖挑战同一关卡连续战败
// 3 次后才结束，而不是首次失败就放弃。
func TestZodiacRoutine_RetriesLossThenStops(t *testing.T) {
	sess, srvConn := createTestSession(t, 100)
	defer sess.Close()
	defer srvConn.Close()

	var challengeCalls atomic.Int32
	startMockDungeonServer(srvConn, func(pkt *protocol.Packet) bool {
		switch pkt.ActionID {
		case protocol.ActionIDZodiacInfo:
			_ = protocol.WritePacket(srvConn, protocol.BuildZodiacInfoResultPacket(protocol.ZodiacInfo{
				NowZodiacLevel: 1, NowBarrier: 1, NextZodiacLevel: 1, NextBarrier: 2,
				RequireLevel: 1, Heaven: 1,
			}))
		case protocol.ActionIDZodiacChallenge:
			challengeCalls.Add(1)
			_ = protocol.WritePacket(srvConn, protocol.BuildZodiacChallengeResultPacket(
				protocol.ZodiacResultFail, 1, 1, 0))
		default:
			return false
		}
		return true
	})

	if err := dungeon.NewZodiacRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("生肖失败重试流程异常: %v", err)
	}
	if got := challengeCalls.Load(); got != 3 {
		t.Fatalf("连续战败应重试到 3 次上限, 实际 %d 次", got)
	}
}
