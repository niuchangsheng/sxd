// Package minigame 提供摘仙桃自动化测试。
//
// @author Ateng
// @since 2026-11-07
package minigame

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestGetPeachRoutine_HarvestAndCall(t *testing.T) {
	var infoCalls, playerCalls, harvestCalls, callCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			infoCalls.Add(1)
			peachNum := uint8(0)
			if infoCalls.Load() == 1 || infoCalls.Load() == 3 {
				peachNum = 1
			}
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: peachNum, CanCall: true,
			}))
		case protocol.ActionIDGetPeachPlayerInfo:
			playerCalls.Add(1)
			coinTimes := uint8(1)
			if playerCalls.Load() > 1 {
				coinTimes = 0
			}
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPlayerInfoResultPacket(protocol.GetPeachPlayerInfo{
				CallCoinTimes: coinTimes, CallIngotTimes: 0,
				CallNeedCoin: 10000000, CallNeedIngot: 100,
			}))
		case protocol.ActionIDPlayerGetWarCdTime:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerGetWarCdTimeResultPacket(0))
		case protocol.ActionIDGetPeachHarvest:
			harvestCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachHarvestResultPacket(protocol.GetPeachResultSuccess, 1000))
		case protocol.ActionIDGetPeachCall:
			callCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachCallResultPacket(protocol.GetPeachResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := harvestCalls.Load(); got != 2 {
		t.Fatalf("应摘取存量 1 次 + 召唤后 1 次, 实际 %d 次", got)
	}
	if got := callCalls.Load(); got != 1 {
		t.Fatalf("应召唤 1 次, 实际 %d 次", got)
	}
}

func TestGetPeachRoutine_NoCoinTimesSkipsCall(t *testing.T) {
	var harvestCalls, callCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: 0, CanCall: true,
			}))
		case protocol.ActionIDGetPeachPlayerInfo:
			// call_coin_times == 0 时只剩元宝档，绝不能召唤。
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPlayerInfoResultPacket(protocol.GetPeachPlayerInfo{
				CallCoinTimes: 0, CallIngotTimes: 5, CallNeedIngot: 100,
			}))
		case protocol.ActionIDGetPeachHarvest:
			harvestCalls.Add(1)
		case protocol.ActionIDGetPeachCall:
			callCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := callCalls.Load(); got != 0 {
		t.Fatalf("无铜钱/免费次数时不得召唤, 实际 %d 次", got)
	}
	if got := harvestCalls.Load(); got != 0 {
		t.Fatalf("没有仙桃时不得摘取, 实际 %d 次", got)
	}
}

func TestGetPeachRoutine_CallFailureStops(t *testing.T) {
	var callCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: 0, CanCall: true,
			}))
		case protocol.ActionIDGetPeachPlayerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPlayerInfoResultPacket(protocol.GetPeachPlayerInfo{
				CallCoinTimes: 1, CallNeedCoin: 10000000,
			}))
		case protocol.ActionIDGetPeachCall:
			callCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachCallResultPacket(protocol.GetPeachResultFailure))
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := callCalls.Load(); got != 1 {
		t.Fatalf("召唤失败后应立即停止, 实际调用 %d 次", got)
	}
}

func TestGetPeachRoutine_HarvestFailureStops(t *testing.T) {
	var harvestCalls, callCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: 1, CanCall: false,
			}))
		case protocol.ActionIDPlayerGetWarCdTime:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerGetWarCdTimeResultPacket(0))
		case protocol.ActionIDGetPeachHarvest:
			harvestCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachHarvestResultPacket(
				protocol.GetPeachResultNotEnoughPeachCount, 0))
		case protocol.ActionIDGetPeachPlayerInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPlayerInfoResultPacket(protocol.GetPeachPlayerInfo{
				CallCoinTimes: 1,
			}))
		case protocol.ActionIDGetPeachCall:
			callCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := harvestCalls.Load(); got != 1 {
		t.Fatalf("摘取失败后应熔断, 实际 %d 次", got)
	}
	if got := callCalls.Load(); got != 0 {
		t.Fatalf("面板不允许召唤时不得调用, 实际 %d 次", got)
	}
}

func TestGetPeachRoutine_WaitsForWarCDThenHarvests(t *testing.T) {
	var cdQueries, harvestCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: 1, CanCall: false,
			}))
		case protocol.ActionIDPlayerGetWarCdTime:
			// 第一次返回 1 秒冷却，等待后的复查返回 0。
			if cdQueries.Add(1) == 1 {
				_ = protocol.WritePacket(conn, protocol.BuildPlayerGetWarCdTimeResultPacket(1))
			} else {
				_ = protocol.WritePacket(conn, protocol.BuildPlayerGetWarCdTimeResultPacket(0))
			}
		case protocol.ActionIDGetPeachHarvest:
			harvestCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachHarvestResultPacket(protocol.GetPeachResultSuccess, 1000))
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := harvestCalls.Load(); got != 1 {
		t.Fatalf("等待冷却后应摘取 1 次, 实际 %d 次", got)
	}
	if got := cdQueries.Load(); got < 2 {
		t.Fatalf("冷却后应复查, 实际查询 %d 次", got)
	}
}

func TestGetPeachRoutine_WarCDTooLongSkipsHarvest(t *testing.T) {
	var harvestCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: 2, CanCall: false,
			}))
		case protocol.ActionIDPlayerGetWarCdTime:
			// 超过 maxGetPeachWarCDWait，应放弃本轮而非长时间阻塞。
			_ = protocol.WritePacket(conn, protocol.BuildPlayerGetWarCdTimeResultPacket(60))
		case protocol.ActionIDGetPeachHarvest:
			harvestCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := harvestCalls.Load(); got != 0 {
		t.Fatalf("战斗冷却过长时不得继续摘取, 实际 %d 次", got)
	}
}

func TestGetPeachRoutine_CallTimesNotDecreaseCircuitBreaker(t *testing.T) {
	var callCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGetPeachInfo:
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPanelResultPacket(protocol.GetPeachPanel{
				PeachLv: 2, PeachNum: 0, CanCall: true,
			}))
		case protocol.ActionIDGetPeachPlayerInfo:
			// 服务端异常: 召唤成功后次数不递减。
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachPlayerInfoResultPacket(protocol.GetPeachPlayerInfo{
				CallCoinTimes: 1, CallNeedCoin: 10000000,
			}))
		case protocol.ActionIDGetPeachCall:
			callCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGetPeachCallResultPacket(protocol.GetPeachResultSuccess))
		}
	})
	defer sess.Close()

	if err := NewGetPeachRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("get peach: %v", err)
	}
	if got := callCalls.Load(); got != 1 {
		t.Fatalf("次数未递减时应熔断, 实际调用 %d 次", got)
	}
}
