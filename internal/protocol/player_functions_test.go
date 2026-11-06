package protocol

import (
	"testing"
)

// TestBuildGetPlayerFunctionsPacket 验证已开放功能查询封包携带动态 PrevAct。
func TestBuildGetPlayerFunctionsPacket(t *testing.T) {
	pkt := BuildGetPlayerFunctionsPacket(0x00000044)
	if pkt.ActionID != ActionIDPlayerGetFunctions {
		t.Fatalf("expected action 0x%08X, got 0x%08X", ActionIDPlayerGetFunctions, pkt.ActionID)
	}
	if !pkt.HasPrevAct {
		t.Fatal("功能开放列表查询必须标记 HasPrevAct")
	}
	if len(pkt.Payload) != 4 {
		t.Fatalf("expected 4-byte PrevAct payload, got %d", len(pkt.Payload))
	}
}

// TestParsePlayerFunctionsResponse 验证已开放功能列表解析。
func TestParsePlayerFunctionsResponse(t *testing.T) {
	w := NewWriter()
	w.WriteUint16(3)
	w.WriteInt32(FunctionIDBeelzebubTrials)
	w.WriteUint8(1) // 已体验
	w.WriteInt32(FunctionIDTreasureHouse)
	w.WriteUint8(0) // 已开放未体验
	w.WriteInt32(FunctionIDZodiac)
	w.WriteUint8(1)

	functions, err := ParsePlayerFunctionsResponse(w.Bytes())
	if err != nil {
		t.Fatalf("parse player functions failed: %v", err)
	}
	if len(functions) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(functions))
	}
	if functions[0].ID != FunctionIDBeelzebubTrials || !functions[0].Played {
		t.Fatalf("unexpected function #0: %+v", functions[0])
	}
	if functions[1].ID != FunctionIDTreasureHouse || functions[1].Played {
		t.Fatalf("unexpected function #1: %+v", functions[1])
	}
}

// TestParsePlayerFunctionsResponse_Truncated 验证截断回包返回错误。
func TestParsePlayerFunctionsResponse_Truncated(t *testing.T) {
	payload := []byte{0x00, 0x01, 0x00, 0x00}
	if _, err := ParsePlayerFunctionsResponse(payload); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}
