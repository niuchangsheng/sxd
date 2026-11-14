package minigame

import (
	"context"
	"encoding/hex"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

const wuxingRoutinePanelHex = "000000001800050404000024a80000001f000016be0000000100000000000003260304000024a900000038000016be0000000100000000000002500204000024aa00000027000016be00000001000000000000025e0504000024ab00000019000016be0000000100000000000002c50104000024ac0000000e000016be00000001000000000000027b006400000101"

// TestWuxingRoutine_OneKeyThenExitOnFailure 验证：一键挑战后刷新面板，
// 只要存在失败关卡（state=3）就结束本轮，不再单独重试/部署。
func TestWuxingRoutine_OneKeyThenExitOnFailure(t *testing.T) {
	panelRaw, err := hex.DecodeString(wuxingRoutinePanelHex)
	if err != nil {
		t.Fatalf("panel hex: %v", err)
	}
	failedPanel := append([]byte(nil), panelRaw...)
	failedPanel[8] = wuxingStateFailed // 第 1 个条目（element=4）置为失败

	var panelCalls, fightCalls, deployCalls atomic.Int32

	sess := newMinigameMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDWuxingPanel10:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWuxingPanel:
			n := panelCalls.Add(1)
			body := panelRaw
			if n > 1 {
				body = failedPanel
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, body))
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
	if fightCalls.Load() != 1 {
		t.Fatalf("期望仅 1 次一键挑战, 实际 %d", fightCalls.Load())
	}
	if deployCalls.Load() != 0 {
		t.Fatalf("按用户要求不做阵容部署, 实际 %d 次", deployCalls.Load())
	}
	if panelCalls.Load() < 2 {
		t.Fatalf("挑战后应刷新面板, panel calls=%d", panelCalls.Load())
	}
}

// TestWuxingRoutine_FightDroppedExits 验证一键挑战被静默丢弃时直接结束、不重试。
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

	if err := NewWuxingTiangongRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuxing routine: %v", err)
	}
	if fightCalls.Load() != 1 {
		t.Fatalf("一键挑战无回包后不得重试, 实际 %d 次", fightCalls.Load())
	}
}
