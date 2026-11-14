package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// 2026-11-15 /tmp/henduo.pcapng 无尽洞天面板（layer=23 出现在第 2 个 Int）。
const endlessPanelHex = "0000000bb40000001700000000000071e2000001fc0000000200001bee000000000000005b000000130000000000010000000a00000064000000010000000400000008"

func TestParseEndlessPanel(t *testing.T) {
	raw, err := hex.DecodeString(endlessPanelHex)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	panel, err := protocol.ParseEndlessPanel(raw)
	if err != nil {
		t.Fatalf("ParseEndlessPanel: %v", err)
	}
	if panel.Result != protocol.EndlessSuccess || panel.Layer() != 23 {
		t.Fatalf("面板不符: result=%d layer=%d", panel.Result, panel.Layer())
	}
}
