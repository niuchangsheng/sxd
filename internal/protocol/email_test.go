// Package protocol_test 提供官方客户端 Mod_Email_Base 邮箱协议编解码的测试用例。
package protocol_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestEmailOneKeyGetAward_Packet(t *testing.T) {
	pkt := protocol.BuildEmailOneKeyGetAwardPacket(protocol.ActionIDEmailGetInfo)
	if pkt.ActionID != protocol.ActionIDEmailOneKeyGetAward {
		t.Fatalf("ActionID 不匹配: 0x%08X", pkt.ActionID)
	}
	if pkt.ActionID != 0x01590006 {
		t.Fatalf("一键收取协议号应为 0x01590006, 实际 0x%08X", pkt.ActionID)
	}
	if len(pkt.Payload) != 4 {
		t.Fatalf("空请求应仅携带 4 字节 PrevAct, 实际 %d 字节", len(pkt.Payload))
	}
	if got := binary.BigEndian.Uint32(pkt.Payload); got != protocol.ActionIDEmailGetInfo {
		t.Errorf("PrevAct 期望 0x%08X, 实际 0x%08X", protocol.ActionIDEmailGetInfo, got)
	}
}

func TestEmailOneKeyGetAward_DefaultPrevAct(t *testing.T) {
	pkt := protocol.BuildEmailOneKeyGetAwardPacket(0)
	if len(pkt.Payload) != 4 {
		t.Fatalf("默认 PrevAct 载荷长度异常: %d", len(pkt.Payload))
	}
	if got := binary.BigEndian.Uint32(pkt.Payload); got != protocol.ActionIDTownEnter {
		t.Errorf("默认 PrevAct 期望 0x%08X, 实际 0x%08X", protocol.ActionIDTownEnter, got)
	}
}

func TestEmailOneKeyGetAward_Result(t *testing.T) {
	cases := []struct {
		name   string
		code   uint8
		expect string
	}{
		{"success", protocol.EmailResultSuccess, "附件收取成功"},
		{"no_award", protocol.EmailResultNoAward, "没有可领取的附件"},
		{"had_get", protocol.EmailResultHadGet, "附件已领取"},
		{"item_pack_full", protocol.EmailResultItemPackFull, "物品背包已满"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := protocol.ParseEmailOneKeyGetAwardResult([]byte{tc.code})
			if err != nil {
				t.Fatalf("解析结果码失败: %v", err)
			}
			if got != tc.code {
				t.Fatalf("结果码期望 %d, 实际 %d", tc.code, got)
			}
			if text := protocol.EmailResultText(got); text != tc.expect {
				t.Errorf("结果说明期望 %q, 实际 %q", tc.expect, text)
			}
		})
	}
}

func TestEmailOneKeyGetAward_ResultEmpty(t *testing.T) {
	if _, err := protocol.ParseEmailOneKeyGetAwardResult(nil); !errors.Is(err, protocol.ErrInvalidPayload) {
		t.Fatalf("空回包应返回 ErrInvalidPayload, 实际: %v", err)
	}
}

func TestEmailGetInfoAndIsHaveNew_Packet(t *testing.T) {
	info := protocol.BuildEmailGetInfoPacket(protocol.ActionIDTownEnter)
	if info.ActionID != 0x01590000 || len(info.Payload) != 4 {
		t.Fatalf("邮箱面板查询封包异常: action=0x%08X len=%d", info.ActionID, len(info.Payload))
	}

	statePkt := protocol.BuildEmailIsHaveNewPacket(protocol.ActionIDEmailGetInfo)
	if statePkt.ActionID != 0x01590005 || len(statePkt.Payload) != 4 {
		t.Fatalf("新邮件查询封包异常: action=0x%08X len=%d", statePkt.ActionID, len(statePkt.Payload))
	}
	state, err := protocol.ParseEmailIsHaveNewResult([]byte{protocol.EmailStateUnread})
	if err != nil || state != protocol.EmailStateUnread {
		t.Fatalf("解析新邮件状态失败: state=%d err=%v", state, err)
	}
}
