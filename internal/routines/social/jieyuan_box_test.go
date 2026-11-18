package social

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestJieYuanBoxRoutine_SendsBatchOpenRequest 验证官方抓包链路：
// 232/30 列表 -> 528/1 上下文握手 [0x0704, PrevAct] -> 232/34 开箱 [item, count, PrevAct=528/1]
// -> 232/30 库存核对。
func TestJieYuanBoxRoutine_SendsBatchOpenRequest(t *testing.T) {
	var listCalls, ctxCalls, openCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			n := listCalls.Add(1)
			w := protocol.NewWriter()
			if n == 1 {
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			} else {
				w.WriteUint16(0)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			ctxCalls.Add(1)
			// 抓包中官方 528/1 为 [0x07 0x04][PrevAct]；会话层用最后收包覆盖尾部。
			// 兜底裸 2 字节版本也接受。
			got := hex.EncodeToString(pkt.Payload)
			if got != "0704" && !(len(pkt.Payload) == 6 && got[:4] == "0704") {
				t.Errorf("528/1 设置载荷 = %s, want 0704 或 0704+PrevAct", got)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanOpenContextResultOK}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
			if len(pkt.Payload) != 12 {
				t.Errorf("开箱载荷长度 = %d, want 12", len(pkt.Payload))
			} else {
				if q := int32(binary.BigEndian.Uint32(pkt.Payload[4:8])); q != protocol.JieYuanBoxAutoMergeQuality {
					t.Errorf("开箱第二参数 = %d, want 自动合成档位 %d", q, protocol.JieYuanBoxAutoMergeQuality)
				}
				if got := binary.BigEndian.Uint32(pkt.Payload[8:12]); got != protocol.ActionIDJieYuanBoxOpenContext {
					t.Errorf("开箱 PrevAct = 0x%08X, want 0x%08X", got, protocol.ActionIDJieYuanBoxOpenContext)
				}
			}
			// 官方抓包回包解压后首字节为成功码 5。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanBoxOpenResultSuccess, 0x00, 0x00}))
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	if ctxCalls.Load() != 1 {
		t.Fatalf("每次开箱前应发送 1 次 528/1 上下文握手, 实际 %d 次", ctxCalls.Load())
	}
	if openCalls.Load() != 1 {
		t.Fatalf("应合并为 1 次批量开箱, 实际 %d 次", openCalls.Load())
	}
}

// TestJieYuanBoxRoutine_StopsOnOpenFailureCode 验证开箱失败码不会被当作成功继续下一批。
func TestJieYuanBoxRoutine_StopsOnOpenFailureCode(t *testing.T) {
	var listCalls, openCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			n := listCalls.Add(1)
			w := protocol.NewWriter()
			if n == 1 {
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			} else {
				// 失败码下库存数量不应变化，若实现继续核对就能暴露异常。
				w.WriteUint16(1)
				w.WriteInt32(protocol.ItemJieYuanBoxCommon)
				w.WriteInt32(2)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanOpenContextResultOK}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
			// 非成功结果码（如次数不足/条件不满足）。
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x01}))
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	if openCalls.Load() != 1 {
		t.Fatalf("失败码后不得继续开箱, 开箱次数 = %d", openCalls.Load())
	}
	if listCalls.Load() != 1 {
		t.Fatalf("失败码后不应进入库存核对, 列表查询次数 = %d", listCalls.Load())
	}
}

// TestJieYuanBoxRoutine_StopsOnContextReject 验证 528/1 握手被拒绝时不得发送开箱请求。
func TestJieYuanBoxRoutine_StopsOnContextReject(t *testing.T) {
	var openCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(
				[]int32{protocol.FunctionIDLinkFate}))
		case protocol.ActionIDJieYuanBoxList:
			w := protocol.NewWriter()
			w.WriteUint16(1)
			w.WriteInt32(protocol.ItemJieYuanBoxCommon)
			w.WriteInt32(2)
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0x01}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
		}
	})
	defer sess.Close()

	err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil)
	if err == nil {
		t.Fatal("握手被拒绝时应返回冷却错误")
	}
	if openCalls.Load() != 0 {
		t.Fatalf("握手被拒绝后不得发送开箱请求, 实际 %d 次", openCalls.Load())
	}
}

// TestJieYuanBoxRoutine_StillListsWhenFunctionClosed 验证功能列表缺少 140 时
// 不直接跳过，而是继续查询宝箱列表（2026-11-18 用户反馈 s1 仍需开箱）；
// 列表为空时不会发起开箱握手。
func TestJieYuanBoxRoutine_StillListsWhenFunctionClosed(t *testing.T) {
	var listCalls, ctxCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			// 功能 140 未在列表中下发。
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(nil))
		case protocol.ActionIDJieYuanBoxList:
			listCalls.Add(1)
			w := protocol.NewWriter()
			w.WriteUint16(0) // 空背包
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, w.Bytes()))
		case protocol.ActionIDJieYuanBoxOpenContext:
			ctxCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if listCalls.Load() == 0 {
		t.Fatal("功能列表缺少 140 时仍应查询结缘宝箱列表")
	}
	if ctxCalls.Load() != 0 {
		t.Fatalf("空背包不应发起开箱握手, ctx=%d", ctxCalls.Load())
	}
}
