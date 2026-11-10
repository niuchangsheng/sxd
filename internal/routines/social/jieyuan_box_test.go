package social

import (
	"context"
	"encoding/binary"
	"net"
	"sync/atomic"
	"testing"

	"sxd-pie-ng/internal/protocol"
)

// TestJieYuanBoxRoutine_SendsBatchOpenRequest 验证 232/34 开箱请求
// [Int item_id, Int count, PrevAct] 的编码与库存核对流程。
func TestJieYuanBoxRoutine_SendsBatchOpenRequest(t *testing.T) {
	var listCalls, openCalls atomic.Int32
	sess := newSocialMockSession(t, func(conn net.Conn, pkt *protocol.Packet) {
		switch pkt.ActionID {
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
		case protocol.ActionIDJieYuanBoxOpen:
			openCalls.Add(1)
			if len(pkt.Payload) != 12 {
				t.Errorf("开箱载荷长度 = %d, want 12", len(pkt.Payload))
			} else if got := binary.BigEndian.Uint32(pkt.Payload[8:12]); got != protocol.ActionIDJieYuanBoxList {
				t.Errorf("开箱 PrevAct = 0x%08X, want 0x%08X", got, protocol.ActionIDJieYuanBoxList)
			}
			_ = protocol.WritePacket(conn, protocol.NewPacket(pkt.ActionID, []byte{0}))
		}
	})
	defer sess.Close()

	if err := NewJieYuanBoxRoutine().Execute(context.Background(), sess, nil); err != nil {
		t.Fatalf("jieyuan box: %v", err)
	}
	if openCalls.Load() != 1 {
		t.Fatalf("应合并为 1 次批量开箱, 实际 %d 次", openCalls.Load())
	}
}
