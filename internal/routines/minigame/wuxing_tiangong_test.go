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

func wuxingLineupBytes() []byte {
	build := func(roleID int32, name string) []byte {
		out := []byte{byte(roleID >> 24), byte(roleID >> 16), byte(roleID >> 8), byte(roleID), 8}
		out = append(out, make([]byte, 8)...) // 属性字段填充
		out = append(out, byte(len(name)>>8), byte(len(name)))
		return append(out, []byte(name)...)
	}
	raw := build(1080352, "遁一")
	raw = append(raw, build(1006659, "李逍遥")...)
	raw = append(raw, build(768553, "佛玲珑")...)
	return raw
}

// TestWuxingRoutine_OneKeyThenRetry 验证：一键挑战后刷新面板，失败条目按
// 「镜像」阵容（玲珑 pos8 + 主角 pos9）部署并单独重试。
func TestWuxingRoutine_OneKeyThenRetry(t *testing.T) {
	panelRaw, err := hex.DecodeString(wuxingRoutinePanelHex)
	if err != nil {
		t.Fatalf("panel hex: %v", err)
	}
	failedPanel := append([]byte(nil), panelRaw...)
	failedPanel[8] = wuxingStateFailed // 第 1 个条目（element=4）置为失败

	var panelCalls, fightCalls, deployCalls atomic.Int32
	var lastFightMode atomic.Int32
	var deployLog [][2]int32 // {role_id, pos}

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
		case protocol.ActionIDWuxingLineup:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, wuxingLineupBytes()))
		case protocol.ActionIDWuxingDeploy:
			deployCalls.Add(1)
			if len(pkt.Payload) >= 5 {
				roleID := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 | uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3]))
				deployLog = append(deployLog, [2]int32{roleID, int32(pkt.Payload[4])})
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDWuxingFight:
			fightCalls.Add(1)
			if len(pkt.Payload) > 0 {
				lastFightMode.Store(int32(pkt.Payload[0]))
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
	})
	defer sess.Close()

	if err := NewWuxingTiangongRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("wuxing routine: %v", err)
	}
	if fightCalls.Load() != 2 {
		t.Fatalf("期望 一键挑战 + 1 次失败重试 = 2 次, 实际 %d", fightCalls.Load())
	}
	if lastFightMode.Load() != int32(protocol.WuxingFightModeRetry) {
		t.Fatalf("失败重试 mode 期望 %d, 实际 %d", protocol.WuxingFightModeRetry, lastFightMode.Load())
	}
	if deployCalls.Load() != 2 {
		t.Fatalf("镜像阵容应部署 2 个号位, 实际 %d", deployCalls.Load())
	}
	if deployLog[0] != [2]int32{768553, 8} || deployLog[1] != [2]int32{1080352, 9} {
		t.Fatalf("镜像部署不符: %v", deployLog)
	}
	if panelCalls.Load() < 2 {
		t.Fatalf("挑战后应刷新面板, panel calls=%d", panelCalls.Load())
	}
}
