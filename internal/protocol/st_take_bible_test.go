// Package protocol_test 验证仙界跨服西天取经 (Mod 114) 回包解析与封包构造。
//
// @author Ateng
// @since 2026-11-02
package protocol_test

import (
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestStTakeBibleCross_ActionIDs(t *testing.T) {
	cases := []struct {
		got  uint32
		want uint32
	}{
		{protocol.ActionIDStTakeBibleCrossOpen, 0x00720000},
		{protocol.ActionIDStTakeBibleCrossGetInfo, 0x00720002},
		{protocol.ActionIDStTakeBibleCrossRefresh, 0x00720008},
		{protocol.ActionIDStTakeBibleCrossStart, 0x0072000A},
		{protocol.ActionIDStTakeBibleCrossUpdateData, 0x0072000F},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("action id mismatch: got 0x%08X want 0x%08X", c.got, c.want)
		}
	}
}

func TestParseStTakeBibleCrossInfoResult(t *testing.T) {
	w := protocol.NewWriter()
	// 2 个使者选项
	w.WriteUint16(2)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionBaiLongMa)
	w.WriteInt8(1)
	w.WriteInt32(1000)
	w.WriteInt16(10)
	w.WriteInt16(20)
	w.WriteInt8(5)
	w.WriteInt16(30)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionTangSeng)
	w.WriteInt8(2)
	w.WriteInt32(5000)
	w.WriteInt16(50)
	w.WriteInt16(60)
	w.WriteInt8(15)
	w.WriteInt16(70)
	// 1 个可保护好友
	w.WriteUint16(1)
	w.WriteInt32(65536)
	w.WriteString("曹植始.s1")
	w.WriteInt8(2)
	w.WriteInt8(3)
	w.WriteInt8(1)
	// 尾部字段
	w.WriteUint8(1)  // takeBibleTimes
	w.WriteInt8(3)   // totalTakeBibleTimes
	w.WriteInt16(20) // refreshPrice
	w.WriteInt8(2)   // takeBibleStatus
	w.WriteUint8(protocol.StTakeBibleCrossProtectionShaWuJing)
	w.WriteString("曹植始.s1")
	w.WriteInt8(1) // bless
	w.WriteInt8(2) // buff

	info, err := protocol.ParseStTakeBibleCrossInfoResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossInfoResult failed: %v", err)
	}
	if len(info.ProtectionList) != 2 || len(info.ProtectPlayerList) != 1 {
		t.Fatalf("unexpected list sizes: %+v", info)
	}
	if info.ProtectionList[1].Protection != protocol.StTakeBibleCrossProtectionTangSeng ||
		info.ProtectionList[1].AwardCoin != 5000 || info.ProtectionList[1].NpcExpe != 70 {
		t.Errorf("unexpected protection record: %+v", info.ProtectionList[1])
	}
	if info.ProtectPlayerList[0].PlayerID != 65536 ||
		info.ProtectPlayerList[0].NickName != "曹植始.s1" ||
		info.ProtectPlayerList[0].RemainProtectTimes != 2 {
		t.Errorf("unexpected protect player: %+v", info.ProtectPlayerList[0])
	}
	if info.TakeBibleTimes != 1 || info.TotalTakeBibleTimes != 3 || info.RefreshPrice != 20 {
		t.Errorf("unexpected tail counters: %+v", info)
	}
	if info.TakeBibleStatus != 2 || info.CanProtection != protocol.StTakeBibleCrossProtectionShaWuJing {
		t.Errorf("unexpected status/protection: %+v", info)
	}
	if info.ProtectPlayer != "曹植始.s1" || info.Bless != 1 || info.Buff != 2 {
		t.Errorf("unexpected tail fields: %+v", info)
	}
}

func TestParseStTakeBibleCrossStartResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
	w.WriteInt8(4)
	msg, itemID, err := protocol.ParseStTakeBibleCrossStartResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossStartResult failed: %v", err)
	}
	if msg != protocol.StTakeBibleCrossMsgSuccess || itemID != 4 {
		t.Errorf("unexpected start result: msg=%d item=%d", msg, itemID)
	}

	// 失败回包可能只含消息码
	w2 := protocol.NewWriter()
	w2.WriteUint8(protocol.StTakeBibleCrossMsgFullTakeBibleTimes)
	msg, itemID, err = protocol.ParseStTakeBibleCrossStartResult(w2.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossStartResult (msg only) failed: %v", err)
	}
	if msg != protocol.StTakeBibleCrossMsgFullTakeBibleTimes || itemID != 0 {
		t.Errorf("unexpected msg-only result: msg=%d item=%d", msg, itemID)
	}
}

func TestParseStTakeBibleCrossRefreshResult(t *testing.T) {
	w := protocol.NewWriter()
	w.WriteUint8(protocol.StTakeBibleCrossMsgSuccess)
	w.WriteUint8(protocol.StTakeBibleCrossProtectionTangSeng)
	w.WriteInt16(40)
	w.WriteInt8(1)
	msg, protection, nextPrice, err := protocol.ParseStTakeBibleCrossRefreshResult(w.Bytes())
	if err != nil {
		t.Fatalf("ParseStTakeBibleCrossRefreshResult failed: %v", err)
	}
	if msg != protocol.StTakeBibleCrossMsgSuccess || protection != protocol.StTakeBibleCrossProtectionTangSeng || nextPrice != 40 {
		t.Errorf("unexpected refresh result: msg=%d protection=%d next=%d", msg, protection, nextPrice)
	}
}

func TestStTakeBibleCrossProtectionName(t *testing.T) {
	if protocol.StTakeBibleCrossProtectionName(protocol.StTakeBibleCrossProtectionTangSeng) != "唐僧" {
		t.Errorf("unexpected name for TangSeng")
	}
	if protocol.StTakeBibleCrossProtectionName(0) != "未知使者" {
		t.Errorf("unexpected name for unknown protection")
	}
}
