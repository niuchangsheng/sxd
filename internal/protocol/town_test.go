package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestBuildEnterTownPacket_BarePayload 验证 enter_town 仅发送 4 字节 townID。
//
// 2026-11-15 线上探针：裸 4 字节有回包；追加 PrevAct 的 8 字节会被服务端
// 静默丢弃，并导致随后 get_sections 超时。
func TestBuildEnterTownPacket_BarePayload(t *testing.T) {
	pkt := protocol.BuildEnterTownPacket(21)
	if pkt.ActionID != protocol.ActionIDTownEnter {
		t.Fatalf("action = 0x%08X, want 0x%08X", pkt.ActionID, protocol.ActionIDTownEnter)
	}
	if pkt.HasPrevAct {
		t.Fatalf("enter_town 不应标记 HasPrevAct")
	}
	if len(pkt.Payload) != 4 {
		t.Fatalf("payload len = %d, want 4 (% X)", len(pkt.Payload), pkt.Payload)
	}
	if got := int32(uint32(pkt.Payload[0])<<24 | uint32(pkt.Payload[1])<<16 |
		uint32(pkt.Payload[2])<<8 | uint32(pkt.Payload[3])); got != 21 {
		t.Fatalf("townID = %d, want 21", got)
	}
}

// TestBuildEnterTownWithPrevActPacket 验证 enter_town 始终携带 8 字节 PrevAct 链。
//
// 2026-11-15 抓包：官方客户端 PrevAct 取「最后收到的包」，由会话层在发包时
// 覆盖尾部 4 字节；anchor=0 时写入占位值，但仍必须标记 HasPrevAct。
func TestBuildEnterTownWithPrevActPacket(t *testing.T) {
	pkt := protocol.BuildEnterTownWithPrevActPacket(21, protocol.ActionIDMissionGetSections)
	if len(pkt.Payload) != 8 {
		t.Fatalf("payload len = %d, want 8 (% X)", len(pkt.Payload), pkt.Payload)
	}
	if !pkt.HasPrevAct {
		t.Fatalf("enter_town 应标记 HasPrevAct 以便会话层写入真实 PrevAct")
	}
	anchor := uint32(pkt.Payload[4])<<24 | uint32(pkt.Payload[5])<<16 |
		uint32(pkt.Payload[6])<<8 | uint32(pkt.Payload[7])
	if anchor != protocol.ActionIDMissionGetSections {
		t.Fatalf("anchor = 0x%08X", anchor)
	}
	placeholder := protocol.BuildEnterTownWithPrevActPacket(21, 0)
	if len(placeholder.Payload) != 8 || !placeholder.HasPrevAct {
		t.Fatalf("anchor=0 应写占位锚点并标记 HasPrevAct, len=%d prev=%v",
			len(placeholder.Payload), placeholder.HasPrevAct)
	}
}
