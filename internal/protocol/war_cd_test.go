// Package protocol 提供战斗冷却查询协议编解码测试。
//
// @author Ateng
// @since 2026-11-08
package protocol

import (
	"encoding/hex"
	"testing"
)

func TestPlayerGetWarCdTimePacket(t *testing.T) {
	pkt := BuildPlayerGetWarCdTimePacket(WarCdTypePeach, 0x00280001)
	if pkt.ActionID != ActionIDPlayerGetWarCdTime || !pkt.HasPrevAct {
		t.Fatalf("战斗冷却封包类型不符: %+v", pkt)
	}
	// Int32 cd_type=17 + Uint32 prev_act。
	if got := hex.EncodeToString(pkt.Payload); got != "0000001100280001" {
		t.Fatalf("战斗冷却封包载荷不符: %s", got)
	}

	cd, err := ParsePlayerGetWarCdTimeResult(BuildPlayerGetWarCdTimeResultPacket(2).Payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cd != 2 {
		t.Fatalf("冷却秒数不符: %d", cd)
	}
}
