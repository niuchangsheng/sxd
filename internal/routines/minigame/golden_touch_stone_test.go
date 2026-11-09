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

func TestGoldenTouchStoneRoutine_PollsAfterInit(t *testing.T) {
	var statusCalls, initCalls, touchCalls, panelCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGoldenTouchStoneStatus:
			n := statusCalls.Add(1)
			status := protocol.GoldenTouchStoneStatusInit
			// 初始化后服务端需要短暂异步落库；前两次仍返回 INIT_STATUS。
			if n >= 4 {
				status = protocol.GoldenTouchStoneStatusTouch
			}
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneStatusResultPacket(status))
		case protocol.ActionIDGoldenTouchStoneInit:
			initCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDGoldenTouchStoneInit, []byte{protocol.GoldenTouchStoneResultSuccess}))
		case protocol.ActionIDGoldenTouchStoneTouchPanel:
			panelCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneTouchPanelResultPacket(protocol.GoldenTouchStonePanel{
				Level: 2, Exp: 100, ThisWeekLuckValue: 8, TodayTouchNum: 1,
			}))
		case protocol.ActionIDGoldenTouchStoneTouch:
			touchCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneTouchResultPacket(
				protocol.GoldenTouchStoneResultSuccess, 10, 5))
		}
	})
	defer sess.Close()

	if err := NewGoldenTouchStoneRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("点金石任务执行失败: %v", err)
	}
	if initCalls.Load() != 1 {
		t.Fatalf("初始化调用 = %d, want 1", initCalls.Load())
	}
	if statusCalls.Load() < 4 {
		t.Fatalf("初始化后应轮询状态, status calls = %d", statusCalls.Load())
	}
	if panelCalls.Load() != 1 || touchCalls.Load() != 1 {
		t.Fatalf("状态就绪后应点化, panel=%d touch=%d", panelCalls.Load(), touchCalls.Load())
	}
}

func TestGoldenTouchStoneRoutine_InitFailCooldown(t *testing.T) {
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDGoldenTouchStoneStatus:
			_ = protocol.WritePacket(conn, protocol.BuildGoldenTouchStoneStatusResultPacket(
				protocol.GoldenTouchStoneStatusInit))
		case protocol.ActionIDGoldenTouchStoneInit:
			_ = protocol.WritePacket(conn, protocol.NewPacket(
				protocol.ActionIDGoldenTouchStoneInit, []byte{protocol.GoldenTouchStoneResultFail}))
		}
	})
	defer sess.Close()

	err := NewGoldenTouchStoneRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("初始化失败应返回 CooldownError, 实际: %v", err)
	}
}
