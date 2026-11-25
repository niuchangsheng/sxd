// Package protocol_test 验证仙界跨服登录凭据与城镇进入协议编解码。
//
// @author Ateng
// @since 2026-11-02
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestParseStCrossServerInfo(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteString("9x703.sxdweb.xd.com")
	w.WriteUint16(8480)
	w.WriteString("s703")
	w.WriteInt32(1793549704)
	w.WriteString("mock_pass_code")
	w.WriteString("XianJie")

	info, err := protocol.ParseStCrossServerInfo(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStCrossServerInfo failed: %v", err)
	}
	if info.Host != "9x703.sxdweb.xd.com" || info.Port != 8480 {
		t.Errorf("unexpected endpoint: %+v", info)
	}
	if info.ServerName != "s703" || info.Time != 1793549704 {
		t.Errorf("unexpected login fields: %+v", info)
	}
	if info.PassCode != "mock_pass_code" || info.TownSign != "XianJie" {
		t.Errorf("unexpected pass/town: %+v", info)
	}
}

func TestParseStCrossServerInfo_Truncated(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteString("9x703.sxdweb.xd.com")
	w.WriteUint16(8480)
	if _, err := protocol.ParseStCrossServerInfo(w.Bytes()); err == nil {
		t.Fatal("expected error for truncated payload")
	}
}

func TestBuildStTownEnterPacket(t *testing.T) {
	pkt := protocol.BuildStTownEnterPacket(protocol.TownIDXianJie)
	if pkt.ActionID != protocol.ActionIDStTownEnter {
		t.Fatalf("unexpected action id: 0x%08X", pkt.ActionID)
	}
	r := protocol.NewReader(pkt.Payload)
	townID, err := r.ReadInt32()
	if err != nil {
		t.Fatalf("read town id: %v", err)
	}
	if townID != 41 {
		t.Errorf("expected town id 41, got %d", townID)
	}
}

func TestStTownLeaveAndChatChannel_BuildAndParse(t *testing.T) {
	leavePkt := protocol.BuildStTownLeavePacket()
	if leavePkt.ActionID != protocol.ActionIDStTownLeave {
		t.Fatalf("unexpected leave action: 0x%08X", leavePkt.ActionID)
	}
	if leavePkt.EffectivePrevActMode() != protocol.PrevActAutoBusiness {
		t.Fatalf("expected PrevActAutoBusiness on leave_town, got %v", leavePkt.EffectivePrevActMode())
	}
	if code, err := protocol.ParseStTownLeaveResult([]byte{protocol.StTownResultTrue}); err != nil || code != protocol.StTownResultTrue {
		t.Fatalf("expected leave success code 0, got code=%d err=%v", code, err)
	}
	if code, err := protocol.ParseStTownLeaveResult([]byte{protocol.StTownResultFailed}); err != nil || code != protocol.StTownResultFailed {
		t.Fatalf("expected leave failed code 3, got code=%d err=%v", code, err)
	}
	if _, err := protocol.ParseStTownLeaveResult(nil); err == nil {
		t.Fatal("expected error on empty leave_town payload")
	}

	chPkt := protocol.BuildChatEnterStChannelPacket()
	if chPkt.ActionID != protocol.ActionIDChatEnterStChannel {
		t.Fatalf("unexpected enter_st_channel action: 0x%08X", chPkt.ActionID)
	}
	if code, err := protocol.ParseChatEnterStChannelResult([]byte{protocol.ChatChannelResultSuccess}); err != nil || code != 0 {
		t.Fatalf("expected enter_st_channel code 0, got code=%d err=%v", code, err)
	}
	if code, err := protocol.ParseChatEnterStChannelResult([]byte{1}); err != nil || code != 1 {
		t.Fatalf("expected enter_st_channel failure code 1, got code=%d err=%v", code, err)
	}
	if _, err := protocol.ParseChatEnterStChannelResult(nil); err == nil {
		t.Fatal("expected error on empty enter_st_channel payload")
	}
}
