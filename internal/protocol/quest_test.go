// Package protocol_test 提供任务协议编解码与解压缩的单元测试。
//
// @author Ateng
// @since 2026-10-24
package protocol_test

import (
	"encoding/hex"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

func TestQuestProtocol_BuildPackets(t *testing.T) {
	t.Run("BuildGetNPCQuestsPacket", func(t *testing.T) {
		pkt, err := protocol.BuildGetNPCQuestsPacket(protocol.GetNPCQuestsRequest{
			NPCID:   48,
			PrevAct: 0x0000004F,
		})
		if err != nil {
			t.Fatalf("BuildGetNPCQuestsPacket 失败: %v", err)
		}
		if pkt.ActionID != protocol.ActionIDQuestGetNPCQuests {
			t.Errorf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", protocol.ActionIDQuestGetNPCQuests, pkt.ActionID)
		}
		// 校验 Frame 5937 Payload: 000000300000004f
		expectedHex := "000000300000004f"
		if hex.EncodeToString(pkt.Payload) != expectedHex {
			t.Errorf("Payload 不匹配: 期望 %s, 实际 %s", expectedHex, hex.EncodeToString(pkt.Payload))
		}
	})

	t.Run("BuildAcceptQuestPacket", func(t *testing.T) {
		pkt, err := protocol.BuildAcceptQuestPacket(protocol.AcceptQuestRequest{
			QuestID: 145,
			PrevAct: 0x00030009,
		})
		if err != nil {
			t.Fatalf("BuildAcceptQuestPacket 失败: %v", err)
		}
		if pkt.ActionID != protocol.ActionIDQuestAccept {
			t.Errorf("ActionID 不匹配: 期望 0x%08X, 实际 0x%08X", protocol.ActionIDQuestAccept, pkt.ActionID)
		}
		// 校验 Frame 5933 Payload: 0000009100030009
		expectedHex := "0000009100030009"
		if hex.EncodeToString(pkt.Payload) != expectedHex {
			t.Errorf("Payload 不匹配: 期望 %s, 实际 %s", expectedHex, hex.EncodeToString(pkt.Payload))
		}
	})
}

func TestQuestProtocol_ParseResponses(t *testing.T) {
	t.Run("ParseNPCQuestsResponse_Frame5938", func(t *testing.T) {
		// Frame 5938: 0003 00000090 01 00000091 01 00000137 00
		raw, _ := hex.DecodeString("0003000000900100000091010000013700")
		items, err := protocol.ParseNPCQuestsResponse(raw)
		if err != nil {
			t.Fatalf("ParseNPCQuestsResponse 失败: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("期望解析出 3 个任务, 实际 %d 个", len(items))
		}
		if items[0].QuestID != 144 || items[0].State != 1 {
			t.Errorf("Item 0 不匹配: %+v", items[0])
		}
		if items[1].QuestID != 145 || items[1].State != 1 {
			t.Errorf("Item 1 不匹配: %+v", items[1])
		}
		if items[2].QuestID != 311 || items[2].State != 0 {
			t.Errorf("Item 2 不匹配: %+v", items[2])
		}
	})

	t.Run("ParsePlayerQuestListResponse_Frame5940_Zlib", func(t *testing.T) {
		// Frame 5940: 压缩后的数据流
		raw, _ := hex.DecodeString("789c636066e064e06160609800c48c4c0c8c404a8a0102806c4653280d44fb11c20c93112af4a1c21240fc0e21ac09159606e20f088dc5506139209e08379b21074978124275165418e4c02908d585506151207e8f102e4108339a235c7218c9dd5d20060b98890418cd10be3c08130300f83c0c98")
		items, err := protocol.ParsePlayerQuestListResponse(raw)
		if err != nil {
			t.Fatalf("ParsePlayerQuestListResponse 失败: %v", err)
		}
		if len(items) != 12 {
			t.Fatalf("期望解析出 12 个任务, 实际 %d 个", len(items))
		}
		// 验证 Item 1: Quest 144 (夺货), State 1 (已达成)
		if items[0].QuestID != 144 || items[0].State != 1 {
			t.Errorf("Item 0 不匹配: %+v", items[0])
		}
		// 验证 Item 3: Quest 147 (暴戾神兽), State 0 (进行中)
		if items[2].QuestID != 147 || items[2].State != 0 {
			t.Errorf("Item 2 不匹配: %+v", items[2])
		}
		// 验证 Item 10: Quest 138 (二入灵山), State 0 (进行中主线)
		if items[10].QuestID != 138 || items[10].State != 0 {
			t.Errorf("Item 10 不匹配: %+v", items[10])
		}
	})
}
