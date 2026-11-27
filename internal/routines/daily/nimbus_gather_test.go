package daily

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"testing"

	"sxd-pie-ng/internal/protocol"
	"sxd-pie-ng/internal/scheduler"
)

// 2026-11-27 /tmp/lingmai.pcapng 官方面板回包：nimbus=80, remain=30, gain=5。
const nimbusInfoLiveHex = "00000050001e00050004000a0309"

func nimbusInfoPayload(t *testing.T, remain int16) []byte {
	t.Helper()
	raw, err := hex.DecodeString(nimbusInfoLiveHex)
	if err != nil {
		t.Fatalf("decode info: %v", err)
	}
	binary.BigEndian.PutUint16(raw[4:6], uint16(remain))
	return raw
}

func nimbusGatherPayload(result int32, remain int16) []byte {
	w := protocol.NewWriter()
	w.WriteInt32(result)
	w.WriteInt32(0)
	w.WriteInt32(0)
	w.WriteInt16(remain)
	w.WriteInt16(5)
	w.WriteInt32(0)
	return w.Bytes()
}

// TestNimbusGatherRoutine_ConsumesThirtyUses 验证按面板剩余次数连续采集 30 次木葫芦，
// 请求格式为 [Uint8 type=0][Uint32 prevAct]，remain 递减到 0 后停止。
func TestNimbusGatherRoutine_ConsumesThirtyUses(t *testing.T) {
	var gatherCalls int
	var badPayload bool
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDTownEnter:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDNimbusInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, nimbusInfoPayload(t, 30)))
		case protocol.ActionIDNimbusGather:
			gatherCalls++
			if len(pkt.Payload) != 5 || pkt.Payload[0] != protocol.NimbusGourdWood {
				badPayload = true
			}
			remain := int16(30 - gatherCalls)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				nimbusGatherPayload(0, remain)))
		}
	})
	if err := NewNimbusGatherRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gatherCalls != 30 {
		t.Fatalf("gather calls = %d, want 30", gatherCalls)
	}
	if badPayload {
		t.Fatal("采集请求应为 5 字节 [type=0][prevAct]")
	}
}

// TestNimbusGatherRoutine_RemainZeroWaitsNextReset 验证面板剩余 0 时不发起采集，
// 并挂起至次日重置（功能可能仍开放，只是今日次数用尽）。
func TestNimbusGatherRoutine_RemainZeroWaitsNextReset(t *testing.T) {
	var gatherCalls int
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		if pkt.ActionID != protocol.ActionIDNimbusInfo {
			return
		}
		gatherCalls += 0
		_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, nimbusInfoPayload(t, 0)))
	})
	err := NewNimbusGatherRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("remain=0 应返回 CooldownError, got %v", err)
	}
	if gatherCalls != 0 {
		t.Fatalf("remain=0 不应发起采集, got %d", gatherCalls)
	}
}

// TestNimbusGatherRoutine_BusinessFailureStops 验证采集结果码非 0 时立即停止并退避，
// 不再继续消耗后续次数（功能终结/条件不满足）。
func TestNimbusGatherRoutine_BusinessFailureStops(t *testing.T) {
	var gatherCalls int
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDTownEnter:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		case protocol.ActionIDNimbusInfo:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, nimbusInfoPayload(t, 30)))
		case protocol.ActionIDNimbusGather:
			gatherCalls++
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID,
				nimbusGatherPayload(1, 30)))
		}
	})
	err := NewNimbusGatherRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("业务失败应返回 CooldownError, got %v", err)
	}
	if gatherCalls != 1 {
		t.Fatalf("业务失败后应立即停止, gather calls = %d", gatherCalls)
	}
}

// TestNimbusGatherRoutine_PanelSilentDropBacksOff 验证面板无回包（功能未开放/已终结）
// 按连续静默丢包退避，而不是当作成功。
func TestNimbusGatherRoutine_PanelSilentDropBacksOff(t *testing.T) {
	sess := newDailyMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		// 所有请求都不回包。
	})
	err := NewNimbusGatherRoutine().Execute(context.Background(), sess, nil)
	var cd *scheduler.CooldownError
	if !errors.As(err, &cd) {
		t.Fatalf("面板无回包应返回 CooldownError, got %v", err)
	}
}
