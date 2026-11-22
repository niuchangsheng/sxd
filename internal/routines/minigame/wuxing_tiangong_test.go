package minigame

import (
	"context"
	"encoding/hex"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

const wuxingRoutinePanelHex = "000000001800050404000024a80000001f000016be0000000100000000000003260304000024a900000038000016be0000000100000000000002500204000024aa00000027000016be00000001000000000000025e0504000024ab00000019000016be0000000100000000000002c50104000024ac0000000e000016be00000001000000000000027b006400000101"

// TestWuxingRoutine_OneKeyRetriesThenExitOnFailure 验证：一键挑战后刷新面板，
// 仍存在待挑战关卡（state=4）时按用户要求重试，连续 N 轮仍未清空才结束本轮。
func TestWuxingRoutine_OneKeyRetriesThenExitOnFailure(t *testing.T) {
	panelRaw, err := hex.DecodeString(wuxingRoutinePanelHex)
	if err != nil {
		t.Fatalf("panel hex: %v", err)
	}
	// 抓包面板 5 个条目均为 state=4（待挑战）；挑战后原样返回即视为仍有待挑战关卡。

	var panelCalls, fightCalls, deployCalls atomic.Int32

	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWuxingPanel10:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWuxingPanel:
			panelCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, panelRaw))
		case protocol.ActionIDWuxingDeploy:
			deployCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWuxingFight:
			fightCalls.Add(1)
			if len(pkt.Payload) == 0 || pkt.Payload[0] != protocol.WuxingFightModeAll {
				t.Errorf("只应发送一键挑战 mode=%d", protocol.WuxingFightModeAll)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
	})
	defer sess.Close()

	if err := NewWuxingTiangongRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuxing routine: %v", err)
	}
	if got, want := fightCalls.Load(), int32(wuxingMaxFailedRounds); got != want {
		t.Fatalf("连续失败时应重试到上限 %d 次, 实际 %d", want, got)
	}
	if deployCalls.Load() != 0 {
		t.Fatalf("按用户要求不做阵容部署, 实际 %d 次", deployCalls.Load())
	}
	if panelCalls.Load() < int32(wuxingMaxFailedRounds)+1 {
		t.Fatalf("每轮挑战后都应刷新面板, panel calls=%d", panelCalls.Load())
	}
}

// TestWuxingRoutine_RetrySucceeds 验证待挑战关卡清空后立即结束，不再打满上限。
func TestWuxingRoutine_RetrySucceeds(t *testing.T) {
	panelRaw, err := hex.DecodeString(wuxingRoutinePanelHex)
	if err != nil {
		t.Fatalf("panel hex: %v", err)
	}
	// 挑战后全部关卡变为 state=3（已通关）。
	clearedPanel := append([]byte(nil), panelRaw...)
	for _, off := range []int{8, 34, 60, 86, 112} {
		clearedPanel[off] = 3
	}

	var fightCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWuxingPanel10:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWuxingPanel:
			// 第一次挑战后仍未清空，第二次挑战后全部通关。
			body := panelRaw
			if fightCalls.Load() > 1 {
				body = clearedPanel
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, body))
		case protocol.ActionIDWuxingFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
	})
	defer sess.Close()

	if err := NewWuxingTiangongRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuxing routine: %v", err)
	}
	if got := fightCalls.Load(); got != 2 {
		t.Fatalf("首轮未清空后重试一次即完成, 期望 2 次, 实际 %d", got)
	}
}

// TestWuxingRoutine_FightDroppedExits 验证一键挑战被静默丢弃时直接结束、不重试，
// 并返回 1 小时冷却，避免每 10 分钟重复空等。
func TestWuxingRoutine_FightDroppedExits(t *testing.T) {
	panelRaw, _ := hex.DecodeString(wuxingRoutinePanelHex)
	var fightCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWuxingPanel10, protocol.ActionIDWuxingPanel:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, panelRaw))
		case protocol.ActionIDWuxingFight:
			fightCalls.Add(1)
			// 故意不回包。
		}
	})
	defer sess.Close()

	err := NewWuxingTiangongRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("一键挑战无回包应返回 CooldownError, 实际 %v", err)
	}
	if wait := time.Until(cd.Until); wait < 55*time.Minute || wait > 65*time.Minute {
		t.Fatalf("退避时长应在 1 小时附近, 实际 %v", wait)
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("一键挑战无回包后不得重试, 实际 %d 次", fightCalls.Load())
	}
}

// TestWuxingRoutine_AllClearedSkipsFight 验证所有关卡已通关 (state=3) 时
// 不再提交一键挑战。
func TestWuxingRoutine_AllClearedSkipsFight(t *testing.T) {
	panelRaw, _ := hex.DecodeString(wuxingRoutinePanelHex)
	clearedPanel := append([]byte(nil), panelRaw...)
	for _, off := range []int{8, 34, 60, 86, 112} {
		clearedPanel[off] = 3
	}

	var panelCalls, fightCalls atomic.Int32
	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWuxingPanel10:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWuxingPanel:
			panelCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, clearedPanel))
		case protocol.ActionIDWuxingFight:
			fightCalls.Add(1)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
	})
	defer sess.Close()

	if err := NewWuxingTiangongRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuxing routine: %v", err)
	}
	if fightCalls.Load() != 0 {
		t.Fatalf("全部通关后不得提交一键挑战, 实际 %d 次", fightCalls.Load())
	}
	if panelCalls.Load() != 1 {
		t.Fatalf("只应读取一次面板, 实际 %d 次", panelCalls.Load())
	}
}
