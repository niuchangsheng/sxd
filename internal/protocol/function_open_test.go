package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestBuildCheckFunctionOpenPacket(t *testing.T) {
	pkt := protocol.BuildCheckFunctionOpenPacket(protocol.FunctionIDRefinePool, 0x00000002)
	if pkt.ActionID != protocol.ActionIDPlayerCheckFunctionOpen {
		t.Fatalf("ActionID = 0x%08X, want 0x%08X", pkt.ActionID, protocol.ActionIDPlayerCheckFunctionOpen)
	}
	if got, want := hex.EncodeToString(pkt.Payload), "000000a300000002"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
	if !pkt.HasPrevAct {
		t.Fatal("功能开放查询必须标记 HasPrevAct")
	}

	defaultPrev := protocol.BuildCheckFunctionOpenPacket(protocol.FunctionIDRefinePool, 0)
	if got, want := hex.EncodeToString(defaultPrev.Payload), "000000a300010000"; got != want {
		t.Fatalf("default prevAct payload = %s, want %s", got, want)
	}
}

func TestParseCheckFunctionOpenResponse(t *testing.T) {
	open, err := protocol.ParseCheckFunctionOpenResponse([]byte{protocol.PlayerFunctionOpenTrue})
	if err != nil {
		t.Fatalf("parse open flag: %v", err)
	}
	if open != protocol.PlayerFunctionOpenTrue {
		t.Fatalf("open flag = %d, want %d", open, protocol.PlayerFunctionOpenTrue)
	}

	closed, err := protocol.ParseCheckFunctionOpenResponse([]byte{protocol.PlayerFunctionOpenFalse})
	if err != nil {
		t.Fatalf("parse closed flag: %v", err)
	}
	if closed != protocol.PlayerFunctionOpenFalse {
		t.Fatalf("closed flag = %d, want %d", closed, protocol.PlayerFunctionOpenFalse)
	}

	if _, err := protocol.ParseCheckFunctionOpenResponse(nil); err == nil {
		t.Fatal("空回包应返回解析错误")
	}
}
