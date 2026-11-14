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
			// 528/1 set_setting 必须为裸 2 字节 [0x07 0x04]，追加 PrevAct 会被静默丢弃。
			if got, want := hex.EncodeToString(pkt.Payload), "0704"; got != want {
				t.Errorf("528/1 设置载荷 = %s, want %s", got, want)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{protocol.JieYuanOpenContextResultOK}))
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
			if len(pkt.Payload) != 12 {
				t.Errorf("开箱载荷长度 = %d, want 12", len(pkt.Payload))
			} else if got := binary.BigEndian.Uint32(pkt.Payload[8:12]); got != protocol.ActionIDJieYuanBoxOpenContext {
				t.Errorf("开箱 PrevAct = 0x%08X, want 0x%08X", got, protocol.ActionIDJieYuanBoxOpenContext)
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

func TestJieYuanBoxRoutine_SkipsWhenFunctionClosed(t *testing.T) {
	var listCalls, ctxCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
		case protocol.ActionIDPlayerGetFunctions:
			// 功能 140 未开放。
			_ = protocol.WritePacket(conn, protocol.BuildPlayerFunctionsResultPacket(nil))
		case protocol.ActionIDJieYuanBoxList:
			listCalls.Add(1)
		case protocol.ActionIDJieYuanBoxOpenContext:
			ctxCalls.Add(1)
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("功能未开放应静默跳过: %v", err)
	}
	if listCalls.Load() != 0 || ctxCalls.Load() != 0 {
		t.Fatalf("功能未开放不应发宝箱请求, list=%d ctx=%d", listCalls.Load(), ctxCalls.Load())
	}
}
